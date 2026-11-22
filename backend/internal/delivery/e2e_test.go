package delivery

import (
	"context"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/insider-case/smart-campaign-composer/internal/mockprovider"
)

// Uçtan uca: gerçek mock provider (deterministik hata profili, geç kabul
// dahil), iki kanal, gerçek limiter (100/s) ve eşzamanlılık (50).
//
// Kanıtlanan:
//   - Her teslimat terminal durumda biter: sent veya (422'ler) failed.
//   - Provider'ın kabul sayısı = DB'deki sent sayısı: duplicate gönderim yok,
//     geç kabul + timeout + retry penceresi dahil.
//   - Provider tarafında ölçülen en yüksek hız kanal başına ≤ 100/s ve
//     provider'ın 110/s eşiği hiç tetiklenmedi.
func TestEndToEndDeterministicProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("takes a few seconds")
	}
	const n = 250
	pool, _ := fixture(t, n, "sms", "email")
	prov := mockprovider.NewServer("deterministic", quiet,
		mockprovider.WithLatency(func() time.Duration { return 30 * time.Millisecond }),
		mockprovider.WithSlowDelay(600*time.Millisecond))
	ts := httptest.NewServer(prov.Routes())
	defer ts.Close()
	client := NewClient(ts.URL, 300*time.Millisecond) // geç kabul (600 ms) timeout'a düşer
	rec := &startRecorder{next: client, starts: map[string][]time.Time{}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	var wg sync.WaitGroup
	for _, ch := range []string{"sms", "email"} {
		d := NewDispatcher(Config{Channel: ch, RatePerSec: 100, RateMargin: 0.03, Concurrency: 50, BatchSize: 50, Lease: 30 * time.Second,
			PollEvery: 20 * time.Millisecond, Policy: Policy{MaxAttempts: 5, Base: 50 * time.Millisecond, Cap: 500 * time.Millisecond, Rand: func() float64 { return 0.5 }}},
			NewStore(pool), rec, m, quiet)
		wg.Add(1)
		go func() { defer wg.Done(); d.Run(ctx) }()
	}

	start := time.Now()
	for {
		var open int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE status IN ('queued','in_flight','retry_wait')`).Scan(&open)
		if open == 0 {
			break
		}
		// 429'lardaki Retry-After: 1 s'yi beklemeyelim.
		_, _ = pool.Exec(ctx, `UPDATE deliveries SET next_attempt_at = now() WHERE status = 'retry_wait' AND next_attempt_at > now()`)
		if ctx.Err() != nil {
			t.Fatalf("not drained, open=%d", open)
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	elapsed := time.Since(start)

	var sent, failed, other int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE status='sent'), count(*) FILTER (WHERE status='failed'),
		count(*) FILTER (WHERE status NOT IN ('sent','failed')) FROM deliveries`).Scan(&sent, &failed, &other)
	st := providerStats(t, ts.URL)
	accepted := int(st["accepted"].(float64))
	outcomes := st["outcomes"].(map[string]any)
	t.Logf("elapsed=%s sent=%d failed=%d provider=%v", elapsed.Round(time.Millisecond), sent, failed, st)

	if other != 0 || sent+failed != 2*n {
		t.Fatalf("non-terminal rows: sent=%d failed=%d other=%d", sent, failed, other)
	}
	if accepted != sent {
		t.Fatalf("DUPLICATE/LOST: provider accepted %d, DB sent %d", accepted, sent)
	}
	if outcomes["slow_accept"] == nil || outcomes["replayed"] == nil {
		t.Fatalf("scenario did not exercise the idempotency window: %v", outcomes)
	}
	if failed == 0 || outcomes["422"] == nil {
		t.Fatal("expected some permanent 422 failures")
	}
	// Asıl garanti BİZİM tarafımızda: istek başlangıçları herhangi bir kayan
	// 1 s penceresinde kanal başına ≤ 100.
	for ch, ts := range rec.starts {
		if m := maxInWindow(ts, time.Second); m > 100 {
			t.Fatalf("%s: %d request starts in a 1s window", ch, m)
		}
	}
	// Provider VARIŞ zamanlarını ölçer (ağ ve scheduler oynaması dahil).
	// Güvenlik payı sayesinde dışarıdan ölçülen değer de ≤ 100 kalmalı.
	for ch, v := range st["max_rps"].(map[string]any) {
		if v.(float64) > 100 {
			t.Fatalf("%s: provider observed %v req/s", ch, v)
		}
	}
	if len(st["rate_limited"].(map[string]any)) != 0 {
		t.Fatalf("provider rate limit triggered: %v", st["rate_limited"])
	}
	// 2*250 ilk deneme + retry'lar, kanal başına 100/s: en az ~2.5 s sürmeli.
	if elapsed < 2*time.Second {
		t.Fatalf("too fast for 100/s per channel: %s", elapsed)
	}
}

// startRecorder her Send çağrısının başlangıç anını (pacer'dan sonra) kaydeder.
type startRecorder struct {
	next   Sender
	mu     sync.Mutex
	starts map[string][]time.Time
}

func (r *startRecorder) Send(ctx context.Context, j Job) SendResult {
	r.mu.Lock()
	r.starts[j.Channel] = append(r.starts[j.Channel], time.Now())
	r.mu.Unlock()
	return r.next.Send(ctx, j)
}

func maxInWindow(ts []time.Time, w time.Duration) int {
	sorted := slices.SortedFunc(slices.Values(ts), func(a, b time.Time) int { return a.Compare(b) })
	best, j := 0, 0
	for i := range sorted {
		for sorted[i].Sub(sorted[j]) >= w {
			j++
		}
		best = max(best, i-j+1)
	}
	return best
}
