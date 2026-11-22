package llm

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics LLM katmanının Prometheus metrikleri. Etiketlerde kampanya ID'si
// veya brief yok (PII ve cardinality).
type Metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	repairs  *prometheus.CounterVec
	invalid  *prometheus.CounterVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "llm_requests_total", Help: "LLM calls by stage and outcome."}, []string{"stage", "outcome"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "llm_request_duration_seconds", Help: "LLM call latency.",
			Buckets: []float64{0.25, 0.5, 1, 2, 4, 8, 16, 32}}, []string{"stage"}),
		repairs: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "llm_repairs_total", Help: "Repair calls issued after validation failure."}, []string{"stage"}),
		invalid: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "llm_invalid_output_total", Help: "LLM outputs rejected by backend validation."}, []string{"stage", "attempt"}),
	}
	reg.MustRegister(m.requests, m.duration, m.repairs, m.invalid)
	return m
}

// InvalidOutput servis tarafından, validator bir çıktıyı reddettiğinde çağrılır.
// attempt: "first" | "repair".
func (m *Metrics) InvalidOutput(stage Stage, attempt string) {
	m.invalid.WithLabelValues(string(stage), attempt).Inc()
}

type instrumented struct {
	next Provider
	m    *Metrics
	log  *slog.Logger
}

// Instrument her çağrıyı ölçer ve loglar (prompt_version, model, süre, sonuç).
func Instrument(p Provider, m *Metrics, log *slog.Logger) Provider {
	return &instrumented{next: p, m: m, log: log}
}

func (i *instrumented) Segment(ctx context.Context, req SegmentRequest) (Result, error) {
	start := time.Now()
	res, err := i.next.Segment(ctx, req)
	i.record(StageSegment, start, res, err, req.Repair != nil, 0)
	return res, err
}

func (i *instrumented) StreamCopy(ctx context.Context, req CopyRequest, onText func(string)) (Result, error) {
	start := time.Now()
	var first time.Duration
	res, err := i.next.StreamCopy(ctx, req, func(s string) {
		if first == 0 {
			first = time.Since(start) // ilk metin gecikmesi: thinking süresini görünür kılar
		}
		onText(s)
	})
	i.record(StageCopy, start, res, err, req.Repair != nil, first)
	return res, err
}

func (i *instrumented) record(stage Stage, start time.Time, res Result, err error, repair bool, firstText time.Duration) {
	dur := time.Since(start)
	outcome := Outcome(err)
	i.m.requests.WithLabelValues(string(stage), outcome).Inc()
	i.m.duration.WithLabelValues(string(stage)).Observe(dur.Seconds())
	if repair {
		i.m.repairs.WithLabelValues(string(stage)).Inc()
	}
	attrs := []any{"stage", stage, "prompt_version", res.PromptVersion, "model", res.Model,
		"dur_ms", dur.Milliseconds(), "outcome", outcome, "repair", repair}
	if firstText > 0 {
		attrs = append(attrs, "first_text_ms", firstText.Milliseconds())
	}
	if err != nil {
		i.log.Warn("llm call failed", append(attrs, "err", err.Error())...)
	} else {
		i.log.Info("llm call", attrs...)
	}
}
