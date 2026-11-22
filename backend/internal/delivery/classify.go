package delivery

import (
	"math"
	"net/http"
	"strconv"
	"time"
)

// Kind bir gönderim denemesinin sonucudur.
type Kind int

const (
	KindSent   Kind = iota // provider kabul etti (ilk kez ya da aynı anahtarın eski makbuzu)
	KindRetry              // geçici hata; retry_wait'e alınır
	KindFailed             // kalıcı hata veya deneme hakkı bitti; bir daha denenmez
)

// SendResult provider çağrısının ham sonucu. Err != nil ise HTTP cevabı
// alınamamıştır (timeout, bağlantı hatası): sonuç BELİRSİZDİR.
type SendResult struct {
	HTTPStatus        int
	ProviderMessageID string
	RetryAfter        time.Duration // 429'da header'dan; yoksa 0
	Err               error
	Timeout           bool
}

// Decision Classify'ın çıktısı.
type Decision struct {
	Kind   Kind
	Reason string        // metrik etiketi ve last_error: sent, rate_limited, server_error, timeout, network, client_error, max_attempts, bad_response
	Delay  time.Duration // yalnızca KindRetry: bir sonraki denemeye kadar
}

// Policy retry parametreleri. Rand [0,1) döner; testte sabitlenir.
type Policy struct {
	MaxAttempts int
	Base        time.Duration // 1 s
	Cap         time.Duration // 30 s
	Rand        func() float64
}

// Classify BLUEPRINT §6.4 tablosunun kodudur. attempts, BU denemenin
// numarasıdır (claim sırasında artırılmış değer; 1-tabanlı).
//
//	2xx                    -> sent
//	429                    -> retry; Retry-After varsa ona uy, yoksa backoff
//	500/502/503/504, 5xx   -> retry, backoff
//	timeout / bağlantı     -> retry, backoff. Sonuç belirsiz: provider kabul
//	                          etmiş olabilir. Güvenli çünkü bir sonraki deneme
//	                          AYNI Idempotency-Key ile gider (§6.5).
//	400/422 ve diğer 4xx   -> failed, tekrar denenmez (tekrar etmek sonucu değiştirmez)
//	deneme hakkı bitti     -> failed (429 da hak tüketir: sonsuz döngü yok)
func (p Policy) Classify(r SendResult, attempts int) Decision {
	var d Decision
	switch {
	case r.Err != nil && r.Timeout:
		d = Decision{Kind: KindRetry, Reason: "timeout"}
	case r.Err != nil:
		d = Decision{Kind: KindRetry, Reason: "network"}
	case r.HTTPStatus >= 200 && r.HTTPStatus < 300:
		if r.ProviderMessageID == "" {
			// 2xx ama makbuz yok: protokol ihlali. Kabul edildiğini varsaymak
			// yerine tekrar sor; aynı anahtar eski makbuzu döndürür.
			d = Decision{Kind: KindRetry, Reason: "bad_response"}
		} else {
			return Decision{Kind: KindSent, Reason: "sent"}
		}
	case r.HTTPStatus == http.StatusTooManyRequests:
		d = Decision{Kind: KindRetry, Reason: "rate_limited", Delay: r.RetryAfter}
	case r.HTTPStatus >= 500:
		d = Decision{Kind: KindRetry, Reason: "server_error"}
	default: // 400, 422 ve diğer 4xx
		return Decision{Kind: KindFailed, Reason: "client_error"}
	}
	if attempts >= p.MaxAttempts {
		return Decision{Kind: KindFailed, Reason: "max_attempts"}
	}
	if d.Delay <= 0 {
		d.Delay = p.Backoff(attempts)
	}
	return d
}

// Backoff exponential + full jitter: rand(0, min(cap, base * 2^attempt)).
// Full jitter: aynı anda düşen binlerce teslimat aynı anda geri gelmesin
// (thundering herd). Alt sınır 0 olabilir; claim zaten next_attempt_at'e
// bakar ve hız sınırı ayrıca uygulanır.
func (p Policy) Backoff(attempt int) time.Duration {
	ceil := float64(p.Base) * math.Pow(2, float64(attempt))
	if ceil > float64(p.Cap) {
		ceil = float64(p.Cap)
	}
	return time.Duration(p.Rand() * ceil)
}

// ParseRetryAfter saniye veya HTTP-date biçimini okur; 0..60 s'ye sınırlar
// (kötü niyetli/bozuk bir header teslimatı saatlerce bekletmesin).
func ParseRetryAfter(h string, now time.Time) time.Duration {
	if h == "" {
		return 0
	}
	var d time.Duration
	if s, err := strconv.Atoi(h); err == nil {
		d = time.Duration(s) * time.Second
	} else if t, err := http.ParseTime(h); err == nil {
		d = t.Sub(now)
	}
	return min(max(d, 0), 60*time.Second)
}
