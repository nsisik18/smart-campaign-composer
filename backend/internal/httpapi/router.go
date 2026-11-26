// Package httpapi HTTP katmanıdır: routing, request decode, hata formatı,
// SSE. Kural: handler içinde SQL, prompt veya retry kararı olmaz; handler
// servisi çağırır ve sonucu HTTP'ye çevirir.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/insider-case/smart-campaign-composer/internal/campaign"
	"github.com/insider-case/smart-campaign-composer/internal/platform"
)

type Deps struct {
	Log      *slog.Logger
	DB       *pgxpool.Pool
	Registry *prometheus.Registry
	Campaign *campaign.Service
	// StatsEvery pano akışının periyodu (varsayılan 500 ms; testte kısa).
	StatsEvery time.Duration
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer) // panik -> 500, stack trace istemciye dönmez
	r.Use(platform.HTTPMetrics(d.Registry))
	r.Use(requestLog(d.Log))

	r.Get("/health/live", platform.LiveHandler())
	// api yalnızca DB'ye bağımlı. Provider'a worker konuşur; provider
	// düşünce api'yi "not ready" saymak kullanıcı akışını gereksiz keser.
	r.Get("/health/ready", platform.ReadyHandler(platform.Check{Name: "db", Fn: d.DB.Ping}))
	r.Handle("/metrics", platform.MetricsHandler(d.Registry))

	r.Route("/api", func(r chi.Router) {
		if d.Campaign != nil {
			every := d.StatsEvery
			if every == 0 {
				every = 500 * time.Millisecond
			}
			h := campaignHandlers{svc: d.Campaign, statsEvery: every}
			r.Post("/campaigns", h.create)
			r.Get("/campaigns/{id}", h.get)
			r.Post("/campaigns/{id}/analyze", h.analyze)
			r.Post("/campaigns/{id}/generate", h.generate)
			r.Patch("/campaigns/{id}/content", h.editContent)
			r.Post("/campaigns/{id}/approve", h.approve)
			r.Get("/campaigns/{id}/events", h.events)
		}
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			WriteError(w, http.StatusNotFound, "not_found", "route not found", nil)
		})
	})
	return r
}

// WriteError tek hata formatı: {"error":{"code","message","details"}}.
func WriteError(w http.ResponseWriter, status int, code, msg string, details []any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"code": code, "message": msg}
	if details != nil {
		body["details"] = details
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"error": body})
}

func requestLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			if r.URL.Path == "/metrics" || r.URL.Path == "/health/live" {
				return // scrape gürültüsü
			}
			log.Info("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
				"dur_ms", time.Since(start).Milliseconds(), "req_id", middleware.GetReqID(r.Context()))
		})
	}
}
