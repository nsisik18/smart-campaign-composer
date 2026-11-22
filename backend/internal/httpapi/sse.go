package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// sseWriter text/event-stream yazar. Birden çok goroutine'den (olaylar +
// keepalive) güvenle çağrılabilir; her olaydan sonra flush eder.
type sseWriter struct {
	mu sync.Mutex
	w  http.ResponseWriter
	rc *http.ResponseController
}

func newSSE(w http.ResponseWriter) *sseWriter {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // nginx tamponlamasın
	w.WriteHeader(http.StatusOK)
	s := &sseWriter{w: w, rc: http.NewResponseController(w)}
	_ = s.rc.Flush()
	return s
}

func (s *sseWriter) send(name string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		b = []byte(`{"code":"internal"}`)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// JSON tek satırdır (json.Marshal newline üretmez), "data:" bölmeye gerek yok.
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, b)
	_ = s.rc.Flush()
}

// keepalive her interval'de bir SSE yorumu yazar. Claude'un ilk metinden
// önceki düşünme süresinde proxy'lerin bağlantıyı boşta sayıp kesmesini
// önler. Dönen fonksiyon durdurur.
func (s *sseWriter) keepalive(interval time.Duration) func() {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				s.mu.Lock()
				fmt.Fprint(s.w, ": ping\n\n")
				_ = s.rc.Flush()
				s.mu.Unlock()
			}
		}
	}()
	return func() { close(done) }
}
