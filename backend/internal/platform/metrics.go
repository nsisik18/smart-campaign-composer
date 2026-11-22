package platform

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewRegistry her binary için ayrı registry; global DefaultRegisterer
// kullanılmaz ki testlerde çift kayıt paniği olmasın.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return reg
}

func MetricsHandler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

// HTTPMetrics http_request_duration_seconds{route,code} üretir.
// Etiket olarak ham path değil chi route pattern'i kullanılır
// (/api/campaigns/{id}); aksi halde cardinality patlar ve ID sızar.
func HTTPMetrics(reg prometheus.Registerer) func(http.Handler) http.Handler {
	hist := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency by route pattern and status code.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "code"})
	reg.MustRegister(hist)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			hist.WithLabelValues(route, strconv.Itoa(ww.Status())).Observe(time.Since(start).Seconds())
		})
	}
}
