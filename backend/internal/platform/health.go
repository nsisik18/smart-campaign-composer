package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Check bir bağımlılığın erişilebilirliğini doğrular.
type Check struct {
	Name string
	Fn   func(ctx context.Context) error
}

// LiveHandler proses ayakta mı sorusunu cevaplar; bağımlılıklara BAKMAZ.
// DB düştüğünde orkestratörün sağlıklı prosesi öldürmemesi için ayrı tutulur.
func LiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ReadyHandler tüm check'leri 2 s timeout ile çalıştırır; biri bile
// başarısızsa 503 döner. Hata detayı yalnızca kısa mesajdır, stack yok.
func ReadyHandler(checks ...Check) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		res := map[string]string{}
		code := http.StatusOK
		for _, c := range checks {
			if err := c.Fn(ctx); err != nil {
				res[c.Name] = "down: " + err.Error()
				code = http.StatusServiceUnavailable
			} else {
				res[c.Name] = "ok"
			}
		}
		status := "ok"
		if code != http.StatusOK {
			status = "unavailable"
		}
		writeJSON(w, code, map[string]any{"status": status, "checks": res})
	}
}

// HTTPCheck GET url'in 2xx dönmesini bekler (örn. provider /health).
func HTTPCheck(name, url string) Check {
	client := &http.Client{Timeout: 2 * time.Second}
	return Check{Name: name, Fn: func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return &statusErr{resp.StatusCode}
		}
		return nil
	}}
}

type statusErr struct{ code int }

func (e *statusErr) Error() string { return "status " + http.StatusText(e.code) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
