package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// Client mock provider'ın (ya da gerçek bir sağlayıcının) HTTP istemcisi.
type Client struct {
	baseURL string
	http    *http.Client
	timeout time.Duration
	now     func() time.Time
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		// Transport bağlantıları yeniden kullanır; 50 eşzamanlı istek için
		// varsayılan MaxIdleConnsPerHost (2) yetmez, her istek yeni TCP açardı.
		http:    &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 128, MaxConnsPerHost: 256, IdleConnTimeout: 30 * time.Second}},
		timeout: timeout,
		now:     time.Now,
	}
}

type sendBody struct {
	Channel   string  `json:"channel"`
	Recipient string  `json:"recipient"`
	Subject   *string `json:"subject"`
	Body      string  `json:"body"`
}

// Send tek bir HTTP denemesi yapar. Retry KARARI burada verilmez (Classify);
// istemci yalnızca olanı raporlar. Her denemede aynı Idempotency-Key gider.
//
// Timeout istek başına context ile uygulanır (5 s) ve lease'ten (30 s)
// belirgin kısadır: lease dolmadan her deneme ya biter ya da iptal olur.
func (c *Client) Send(ctx context.Context, j Job) SendResult {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	b, _ := json.Marshal(sendBody{Channel: j.Channel, Recipient: j.Recipient, Subject: j.Subject, Body: j.Body})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/send", bytes.NewReader(b))
	if err != nil {
		return SendResult{Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", j.Key)

	resp, err := c.http.Do(req)
	if err != nil {
		var ne net.Error
		timeout := errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
		return SendResult{Err: err, Timeout: timeout}
	}
	defer resp.Body.Close()

	res := SendResult{HTTPStatus: resp.StatusCode}
	if resp.StatusCode == http.StatusTooManyRequests {
		res.RetryAfter = ParseRetryAfter(resp.Header.Get("Retry-After"), c.now())
	}
	if resp.StatusCode/100 == 2 {
		var out struct {
			ProviderMessageID string `json:"provider_message_id"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&out); err == nil {
			res.ProviderMessageID = out.ProviderMessageID
		}
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 16<<10)) // bağlantı yeniden kullanılsın
	}
	return res
}
