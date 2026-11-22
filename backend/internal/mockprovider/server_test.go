package mockprovider

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	s := NewServer("none", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.latency = func() time.Duration { return 0 }
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	return s, ts
}

func send(t *testing.T, url, key, body string) (int, SendResponse) {
	t.Helper()
	b, _ := json.Marshal(SendRequest{Channel: "sms", Recipient: "+905550000001", Body: body})
	req, _ := http.NewRequest(http.MethodPost, url+"/send", bytes.NewReader(b))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out SendResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestSameKeyReturnsSameReceipt(t *testing.T) {
	s, ts := newTestServer(t)
	c1, r1 := send(t, ts.URL, "k1", "hello")
	c2, r2 := send(t, ts.URL, "k1", "hello")
	if c1 != http.StatusAccepted || c2 != http.StatusOK {
		t.Fatalf("codes %d %d", c1, c2)
	}
	if r1.ProviderMessageID == "" || r1.ProviderMessageID != r2.ProviderMessageID || !r2.Duplicate {
		t.Fatalf("receipts differ: %+v %+v", r1, r2)
	}
	if s.store.accepted.Load() != 1 || s.store.calls["k1"] != 2 {
		t.Fatalf("accepted=%d calls=%d", s.store.accepted.Load(), s.store.calls["k1"])
	}
}

func TestMissingKeyRejected(t *testing.T) {
	_, ts := newTestServer(t)
	if c, _ := send(t, ts.URL, "", "hello"); c != http.StatusBadRequest {
		t.Fatalf("code %d", c)
	}
}

func TestKeyReuseWithDifferentBodyRejected(t *testing.T) {
	_, ts := newTestServer(t)
	send(t, ts.URL, "k1", "hello")
	if c, _ := send(t, ts.URL, "k1", "different"); c != http.StatusUnprocessableEntity {
		t.Fatalf("code %d", c)
	}
}

func TestConcurrentSameKeyAcceptedOnce(t *testing.T) {
	s, ts := newTestServer(t)
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, r := send(t, ts.URL, "race", "hello")
			ids <- r.ProviderMessageID
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("different ids under race: %s vs %s", first, id)
		}
	}
	if s.store.accepted.Load() != 1 {
		t.Fatalf("accepted=%d", s.store.accepted.Load())
	}
}
