// Package mockprovider gerçek bir SMS/email sağlayıcısını taklit eden
// bağımsız HTTP servisidir. Bizim DB'ye dokunmaz; tek durumu bellekteki
// receipt map'idir (gerçek bir dış servis gibi).
//
// ── Idempotency sözleşmesi (BLUEPRINT §6.5) ────────────────────────────
// Bu dosyadaki en kritik kısım receiptStore'dur; satır satır inceleme için
// adımlar:
//
//  1. Her POST /send isteği Idempotency-Key header'ı taşımak ZORUNDA. Yoksa
//     400: anahtarsız gönderim idempotent olamaz, sessizce kabul etmeyiz.
//  2. Anahtar daha önce KABUL edildiyse (receipt var) yeni gönderim yapılmaz;
//     aynı provider_message_id 200 ile döner. Worker'ın "provider kabul etti
//     ama ben DB'ye yazamadan öldüm" penceresini kapatan şey budur.
//  3. Aynı body ile gelmesi beklenir. Farklı body ile aynı anahtar gelirse
//     422 döneriz (Stripe'ın davranışı): bu bizim tarafta bir bug demektir
//     ve sessizce yutulmamalı.
//  4. Kabul kararı + receipt yazımı tek mutex altında atomiktir; aynı anahtarla
//     eşzamanlı iki istek gelirse yalnızca biri "yeni kabul" sayılır.
//
// Hata profilleri profile.go'da. Profil ve hata üretimi receipt mantığından
// AYRIDIR: hata dönen bir istek receipt oluşturmaz, bir istisna hariç:
// "kabul et ama cevabı geç dön" (faultSlowAccept). Bu senaryo, worker'ın
// timeout alıp aynı anahtarla tekrar denediği ve provider'ın eski makbuzu
// döndüğü idempotency penceresini canlı gösterir.
package mockprovider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type SendRequest struct {
	Channel   string  `json:"channel"`
	Recipient string  `json:"recipient"`
	Subject   *string `json:"subject"`
	Body      string  `json:"body"`
}

type SendResponse struct {
	ProviderMessageID string `json:"provider_message_id"`
	Duplicate         bool   `json:"duplicate"` // bilgi amaçlı; worker buna güvenmemeli
}

type receipt struct {
	messageID string
	bodyHash  string
}

// receiptStore idempotency anahtarı -> kabul makbuzu. calls, anahtar başına
// kaç HTTP çağrısı geldiğini sayar (Q-06 testi: çağrı 2, kabul 1).
type receiptStore struct {
	mu       sync.Mutex
	receipts map[string]receipt
	calls    map[string]int
	accepted atomic.Int64
	seq      atomic.Int64
}

func newReceiptStore() *receiptStore {
	return &receiptStore{receipts: map[string]receipt{}, calls: map[string]int{}}
}

// recordCall çağrı sayacını artırır ve bu anahtar için kaçıncı çağrı
// olduğunu döner (1-tabanlı). Deterministik hata profili bunu kullanacak.
func (s *receiptStore) recordCall(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[key]++
	return s.calls[key]
}

// acceptOrReplay idempotency kararının tek noktasıdır.
//   - yeni anahtar: receipt yazar, (id, false, nil)
//   - bilinen anahtar + aynı body: (eski id, true, nil)
//   - bilinen anahtar + farklı body: errKeyReuse
func (s *receiptStore) acceptOrReplay(key, bodyHash string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.receipts[key]; ok {
		if r.bodyHash != bodyHash {
			return "", false, errKeyReuse
		}
		return r.messageID, true, nil
	}
	id := fmt.Sprintf("pm_%d_%s", s.seq.Add(1), key[:min(8, len(key))])
	s.receipts[key] = receipt{messageID: id, bodyHash: bodyHash}
	s.accepted.Add(1)
	return id, false, nil
}

var errKeyReuse = fmt.Errorf("idempotency key reused with a different payload")

type Server struct {
	profile string
	store   *receiptStore
	gate    *rateGate
	log     *slog.Logger
	// latency gecikmeyi simüle eder; testte sıfırlanabilir.
	latency func() time.Duration
	// slowDelay "geç kabul" cevabının gecikmesi; worker timeout'undan (5 s) uzun.
	slowDelay time.Duration

	outMu    sync.Mutex
	outcomes map[string]int // 202/200/429/503/422/slow sayaçları (/stats)
}

// Option testlerin gecikmeleri kısaltması için.
type Option func(*Server)

func WithLatency(f func() time.Duration) Option { return func(s *Server) { s.latency = f } }
func WithSlowDelay(d time.Duration) Option      { return func(s *Server) { s.slowDelay = d } }

func NewServer(profile string, log *slog.Logger, opts ...Option) *Server {
	switch profile {
	case "none", "deterministic", "chaos":
	default:
		log.Warn("unknown mock profile, using none", "requested", profile)
		profile = "none"
	}
	s := &Server{
		profile:   profile,
		store:     newReceiptStore(),
		gate:      newRateGate(110),
		log:       log,
		latency:   func() time.Duration { return time.Duration(20+rand.IntN(61)) * time.Millisecond },
		slowDelay: 6 * time.Second,
		outcomes:  map[string]int{},
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *Server) count(outcome string) {
	s.outMu.Lock()
	s.outcomes[outcome]++
	s.outMu.Unlock()
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /send", s.handleSend)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "profile": s.profile})
	})
	mux.HandleFunc("GET /stats", s.handleStats)
	return mux
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeErr(w, http.StatusBadRequest, "missing_idempotency_key")
		return
	}
	var req SendRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if (req.Channel != "sms" && req.Channel != "email") || req.Recipient == "" || req.Body == "" {
		writeErr(w, http.StatusUnprocessableEntity, "invalid_message")
		return
	}

	callNo := s.store.recordCall(key)

	// Sağlayıcının kendi hız sınırı: bizim limiter'ı denetler.
	if !s.gate.admit(req.Channel, time.Now()) {
		s.count("rate_limited")
		w.Header().Set("Retry-After", "1")
		writeErr(w, http.StatusTooManyRequests, "provider_rate_limited")
		return
	}

	time.Sleep(s.latency())

	switch s.decide(key, callNo) {
	case fault429:
		s.count("429")
		w.Header().Set("Retry-After", "1")
		writeErr(w, http.StatusTooManyRequests, "throttled")
		return
	case fault503:
		s.count("503")
		writeErr(w, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	case fault422:
		s.count("422")
		writeErr(w, http.StatusUnprocessableEntity, "recipient_rejected")
		return
	case faultSlowAccept:
		// Önce KABUL et (makbuz yazılır, mesaj "gitti"), sonra cevabı
		// geciktir. İstemci timeout alır ve sonucu bilmez.
		if _, _, err := s.store.acceptOrReplay(key, hashRequest(req)); err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "idempotency_key_reuse")
			return
		}
		s.count("slow_accept")
		select {
		case <-time.After(s.slowDelay):
		case <-r.Context().Done():
			return // istemci vazgeçti; cevap kimseye ulaşmayacak
		}
	}

	id, dup, err := s.store.acceptOrReplay(key, hashRequest(req))
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "idempotency_key_reuse")
		return
	}
	if dup {
		s.count("replayed")
	} else {
		s.count("accepted")
	}
	// Yeni kabul 202, tekrar 200: ikisi de worker için "sent" (2xx).
	code := http.StatusAccepted
	if dup {
		code = http.StatusOK
	}
	writeJSON(w, code, SendResponse{ProviderMessageID: id, Duplicate: dup})
}

// handleStats demo ve smoke test içindir: accepted == DB'deki sent sayısı
// olmalı; max_calls_per_key > 1 retry'ların görünür kanıtıdır.
func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	s.store.mu.Lock()
	totalCalls, maxCalls := 0, 0
	for _, n := range s.store.calls {
		totalCalls += n
		maxCalls = max(maxCalls, n)
	}
	keys := len(s.store.calls)
	s.store.mu.Unlock()
	s.outMu.Lock()
	outcomes := map[string]int{}
	for k, v := range s.outcomes {
		outcomes[k] = v
	}
	s.outMu.Unlock()
	maxRPS, rejected := s.gate.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"profile":           s.profile,
		"accepted":          s.store.accepted.Load(),
		"distinct_keys":     keys,
		"total_calls":       totalCalls,
		"max_calls_per_key": maxCalls,
		"outcomes":          outcomes,
		"max_rps":           maxRPS,   // kanal başına herhangi bir 1 s penceresindeki en yüksek istek sayısı
		"rate_limited":      rejected, // 110/s eşiğini aşan istekler; 0 olmalı
	})
}

func hashRequest(r SendRequest) string {
	h := sha256.New()
	subj := ""
	if r.Subject != nil {
		subj = *r.Subject
	}
	// Alan ayırıcı olarak NUL: "a"+"bc" ile "ab"+"c" çakışmasın.
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s", r.Channel, r.Recipient, subj, r.Body)
	return hex.EncodeToString(h.Sum(nil))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errCode string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"code": errCode}})
}
