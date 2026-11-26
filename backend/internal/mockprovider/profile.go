package mockprovider

import (
	"hash/fnv"
	"sync"
	"time"
)

// fault mock provider'ın bir istekte üreteceği davranıştır.
type fault int

const (
	faultNone       fault = iota
	fault429              // Retry-After: 1
	fault503              // geçici sunucu hatası
	fault422              // kalıcı: alıcı reddedildi; her denemede aynı
	faultSlowAccept       // kabul eder (makbuz yazılır) ama cevabı istemci timeout'undan SONRA döner
)

// Oranlar: ~%10 429, ~%5 503, ~%2 422, ~%2 geç kabul.
// Kümülatif eşikler, 0..99 kovası üzerinden.
const (
	p429  = 10
	p503  = p429 + 5
	p422  = p503 + 2
	pSlow = p422 + 2
)

// decide profil kuralıdır.
//
//   - none: her şey başarılı.
//   - deterministic: kova = fnv(key) % 100. Hata YALNIZCA ilk çağrıda (422
//     hariç: kalıcı olduğu için her çağrıda). Sonuç key + çağrı numarasına
//     bağlı, flaky değil: aynı seed ve aynı kampanya her seferinde aynı
//     teslimatlarda aynı hataları üretir.
func (s *Server) decide(key string, callNo int) fault {
	var bucket int
	switch s.profile {
	case "deterministic":
		h := fnv.New32a()
		_, _ = h.Write([]byte(key))
		bucket = int(h.Sum32() % 100)
		if bucket >= p503 && bucket < p422 {
			return fault422
		}
		if callNo > 1 {
			return faultNone
		}
	default:
		return faultNone
	}
	switch {
	case bucket < p429:
		return fault429
	case bucket < p503:
		return fault503
	case bucket < p422:
		return fault422
	case bucket < pSlow:
		return faultSlowAccept
	}
	return faultNone
}

// rateGate kanal başına kayan 1 s penceresinde gelen istekleri sayar.
// Eşiği aşan istek 429 alır. Bizim limiter'ımız doğru çalışıyorsa (100/s)
// bu eşik (110/s) hiç tetiklenmemeli: /stats'taki rate_limited sayısı 0.
type rateGate struct {
	mu        sync.Mutex
	limit     int
	window    map[string][]time.Time
	maxPerSec map[string]int
	rejected  map[string]int
}

func newRateGate(limit int) *rateGate {
	return &rateGate{limit: limit, window: map[string][]time.Time{}, maxPerSec: map[string]int{}, rejected: map[string]int{}}
}

// admit isteği pencereye ekler; pencere doluysa false.
func (g *rateGate) admit(channel string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	w := g.window[channel]
	cut := 0
	for cut < len(w) && now.Sub(w[cut]) >= time.Second {
		cut++
	}
	w = append(w[cut:], now)
	g.window[channel] = w
	g.maxPerSec[channel] = max(g.maxPerSec[channel], len(w))
	if len(w) > g.limit {
		g.rejected[channel]++
		return false
	}
	return true
}

func (g *rateGate) snapshot() (map[string]int, map[string]int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	a, b := map[string]int{}, map[string]int{}
	for k, v := range g.maxPerSec {
		a[k] = v
	}
	for k, v := range g.rejected {
		b[k] = v
	}
	return a, b
}
