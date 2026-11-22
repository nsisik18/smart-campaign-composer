package delivery

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func policy() Policy {
	return Policy{MaxAttempts: 5, Base: time.Second, Cap: 30 * time.Second, Rand: func() float64 { return 0.5 }}
}

func TestClassifyTable(t *testing.T) {
	p := policy()
	cases := []struct {
		name   string
		r      SendResult
		att    int
		kind   Kind
		reason string
		delay  time.Duration
	}{
		{"accepted", SendResult{HTTPStatus: 202, ProviderMessageID: "pm_1"}, 1, KindSent, "sent", 0},
		{"replayed receipt", SendResult{HTTPStatus: 200, ProviderMessageID: "pm_1"}, 2, KindSent, "sent", 0},
		{"2xx without id", SendResult{HTTPStatus: 200}, 1, KindRetry, "bad_response", time.Second},
		{"429 retry-after", SendResult{HTTPStatus: 429, RetryAfter: 3 * time.Second}, 1, KindRetry, "rate_limited", 3 * time.Second},
		{"429 no header", SendResult{HTTPStatus: 429}, 2, KindRetry, "rate_limited", 2 * time.Second},
		{"503", SendResult{HTTPStatus: 503}, 3, KindRetry, "server_error", 4 * time.Second},
		{"500", SendResult{HTTPStatus: 500}, 1, KindRetry, "server_error", time.Second},
		{"timeout", SendResult{Err: errors.New("deadline"), Timeout: true}, 1, KindRetry, "timeout", time.Second},
		{"network", SendResult{Err: errors.New("refused")}, 1, KindRetry, "network", time.Second},
		{"422", SendResult{HTTPStatus: 422}, 1, KindFailed, "client_error", 0},
		{"400", SendResult{HTTPStatus: 400}, 1, KindFailed, "client_error", 0},
		{"422 is never retried even early", SendResult{HTTPStatus: 422}, 1, KindFailed, "client_error", 0},
		{"503 on last attempt", SendResult{HTTPStatus: 503}, 5, KindFailed, "max_attempts", 0},
		{"429 also consumes attempts", SendResult{HTTPStatus: 429, RetryAfter: time.Second}, 5, KindFailed, "max_attempts", 0},
		{"success on last attempt still sent", SendResult{HTTPStatus: 202, ProviderMessageID: "x"}, 5, KindSent, "sent", 0},
	}
	for _, c := range cases {
		d := p.Classify(c.r, c.att)
		if d.Kind != c.kind || d.Reason != c.reason || d.Delay != c.delay {
			t.Errorf("%s: got %+v", c.name, d)
		}
	}
}

func TestBackoffFullJitterAndCap(t *testing.T) {
	p := policy()
	p.Rand = func() float64 { return 0.999999 }
	for att, want := range map[int]time.Duration{1: 2 * time.Second, 2: 4 * time.Second, 4: 16 * time.Second, 5: 30 * time.Second, 10: 30 * time.Second} {
		if got := p.Backoff(att); got > want || got < want-time.Millisecond {
			t.Errorf("attempt %d: %s want ~%s", att, got, want)
		}
	}
	p.Rand = func() float64 { return 0 }
	if p.Backoff(3) != 0 {
		t.Error("full jitter lower bound is 0")
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	for h, want := range map[string]time.Duration{
		"":                              0,
		"1":                             time.Second,
		"-5":                            0,
		"3600":                          60 * time.Second,
		"garbage":                       0,
		"Sun, 22 Nov 2026 12:00:07 GMT": 7 * time.Second,
	} {
		if got := ParseRetryAfter(h, now); got != want {
			t.Errorf("%q: %s want %s", h, got, want)
		}
	}
}

// RATE-01: sahte saatle, istek başlangıçlarının HERHANGİ bir kayan 1 s
// penceresinde en fazla 100 olduğu; ve boşta beklemenin burst'e izin vermediği.
func TestPacerSlidingWindow(t *testing.T) {
	clock := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	p := NewPacer(100, 0) // payın kendisini değil, limiter'ın sınırını sınıyoruz
	p.now = func() time.Time { return clock }
	p.sleep = func(_ context.Context, d time.Duration) error { clock = clock.Add(d); return nil }

	var starts []time.Time
	for i := 0; i < 1000; i++ {
		if i == 500 {
			clock = clock.Add(10 * time.Second) // uzun boşluk: token birikmemeli
		}
		if err := p.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
		starts = append(starts, clock)
	}
	maxInWindow := 0
	j := 0
	for i := range starts {
		for starts[i].Sub(starts[j]) >= time.Second {
			j++
		}
		maxInWindow = max(maxInWindow, i-j+1)
	}
	if maxInWindow > 100 {
		t.Fatalf("max starts in a 1s window = %d", maxInWindow)
	}
	if maxInWindow < 99 {
		t.Fatalf("pacer too slow: %d per second", maxInWindow)
	}
	for i := 501; i < len(starts); i++ {
		if starts[i].Sub(starts[i-1]) < 10*time.Millisecond {
			t.Fatalf("burst after idle at %d: gap %s", i, starts[i].Sub(starts[i-1]))
		}
	}
}

// Kanallar bağımsız: SMS'in limiter'ı dolu iken email beklemez.
func TestPacersAreIndependent(t *testing.T) {
	clock := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	mk := func() *Pacer {
		p := NewPacer(100, 0)
		p.now = func() time.Time { return clock }
		p.sleep = func(_ context.Context, d time.Duration) error {
			if d > 0 {
				t.Fatalf("unexpected wait %s", d)
			}
			return nil
		}
		return p
	}
	sms, email := mk(), mk()
	_ = sms.Wait(context.Background())
	_ = email.Wait(context.Background()) // aynı anda, beklemeden
}

func TestPacerCancelReleasesSlot(t *testing.T) {
	clock := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	p := NewPacer(1, 0) // 1/s: ikinci istek 1 s beklemeli
	p.now = func() time.Time { return clock }
	p.sleep = func(ctx context.Context, d time.Duration) error {
		if d > 0 {
			return context.Canceled
		}
		return nil
	}
	_ = p.Wait(context.Background())
	if err := p.Wait(context.Background()); err == nil {
		t.Fatal("expected cancel")
	}
}

// Güvenlik payı: %3 ile kayan 1 s penceresinde en fazla 98 başlangıç; her
// başlangıca 20 ms'ye kadar uyanma gecikmesi eklense bile ≤ 100 kalır.
func TestPacerMarginAbsorbsWakeupJitter(t *testing.T) {
	clock := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	p := NewPacer(100, 0.03)
	p.now = func() time.Time { return clock }
	p.sleep = func(_ context.Context, d time.Duration) error { clock = clock.Add(d); return nil }
	var exact, jittered []time.Time
	for i := 0; i < 1000; i++ {
		_ = p.Wait(context.Background())
		exact = append(exact, clock)
		jitter := time.Duration((i*7919)%21) * time.Millisecond // 0..20 ms, sözde rastgele
		jittered = append(jittered, clock.Add(jitter))
	}
	if m := windowMax(exact); m > 98 {
		t.Fatalf("exact starts: %d per window", m)
	}
	if m := windowMax(jittered); m > 100 {
		t.Fatalf("jittered starts: %d per window", m)
	}
}

func windowMax(ts []time.Time) int {
	s := slices.SortedFunc(slices.Values(ts), func(a, b time.Time) int { return a.Compare(b) })
	best, j := 0, 0
	for i := range s {
		for s[i].Sub(s[j]) >= time.Second {
			j++
		}
		best = max(best, i-j+1)
	}
	return best
}
