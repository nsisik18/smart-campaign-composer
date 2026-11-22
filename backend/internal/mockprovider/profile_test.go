package mockprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeterministicProfileIsStableAndHealsOnSecondCall(t *testing.T) {
	s := NewServer("deterministic", slog.New(slog.NewTextHandler(io.Discard, nil)))
	counts := map[fault]int{}
	for i := 0; i < 2000; i++ {
		key := fmt.Sprintf("key-%d", i)
		f1 := s.decide(key, 1)
		if f1 != s.decide(key, 1) {
			t.Fatal("deterministic profile must not be flaky")
		}
		counts[f1]++
		f2 := s.decide(key, 2)
		if f1 == fault422 && f2 != fault422 {
			t.Fatal("422 must be permanent")
		}
		if f1 != fault422 && f2 != faultNone {
			t.Fatalf("second call should succeed, got %v", f2)
		}
	}
	// Oranlar yaklaşık doğru (2000 anahtarda ±%3).
	for f, want := range map[fault]float64{fault429: 0.10, fault503: 0.05, fault422: 0.02, faultSlowAccept: 0.02} {
		if got := float64(counts[f]) / 2000; got < want-0.03 || got > want+0.03 {
			t.Errorf("fault %v ratio %.3f want ~%.2f", f, got, want)
		}
	}
}

// "Geç kabul": istemci cevabı alamadan vazgeçse bile makbuz yazılmıştır;
// aynı anahtarla tekrar gelen istek yeni gönderim yapmadan aynı id'yi alır.
func TestSlowAcceptCreatesReceiptBeforeResponding(t *testing.T) {
	s := NewServer("none", slog.New(slog.NewTextHandler(io.Discard, nil)), WithLatency(func() time.Duration { return 0 }), WithSlowDelay(time.Second))
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	// Deterministik profilde ilk çağrıda faultSlowAccept üreten bir anahtar bul.
	s.profile = "deterministic"
	key := ""
	for i := 0; i < 10000; i++ {
		k := fmt.Sprintf("k%d", i)
		if s.decide(k, 1) == faultSlowAccept {
			key = k
			break
		}
	}
	if key == "" {
		t.Fatal("no slow key found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	b, _ := json.Marshal(SendRequest{Channel: "sms", Recipient: "+905550000001", Body: "hi"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/send", bytes.NewReader(b))
	req.Header.Set("Idempotency-Key", key)
	if _, err := http.DefaultClient.Do(req); err == nil {
		t.Fatal("expected client timeout")
	}
	if s.store.accepted.Load() != 1 {
		t.Fatal("slow accept must create the receipt before responding")
	}
	code, r2 := send(t, ts.URL, key, "hi")
	if code != http.StatusOK || !r2.Duplicate {
		t.Fatalf("retry should replay receipt: %d %+v", code, r2)
	}
	if s.store.accepted.Load() != 1 {
		t.Fatal("retry must not create a second acceptance")
	}
}

func TestRateGate(t *testing.T) {
	g := newRateGate(110)
	t0 := time.Now()
	for i := 0; i < 110; i++ {
		if !g.admit("sms", t0.Add(time.Duration(i)*time.Millisecond)) {
			t.Fatalf("request %d rejected", i)
		}
	}
	if g.admit("sms", t0.Add(200*time.Millisecond)) {
		t.Fatal("111th request in a second must be rejected")
	}
	if !g.admit("email", t0.Add(200*time.Millisecond)) {
		t.Fatal("channels are independent")
	}
	if !g.admit("sms", t0.Add(1200*time.Millisecond)) {
		t.Fatal("window should slide")
	}
}
