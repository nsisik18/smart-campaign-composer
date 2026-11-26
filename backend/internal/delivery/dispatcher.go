package delivery

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Sender ve JobStore dispatcher'ın bağımlılıkları; testlerde değiştirilebilir.
type Sender interface {
	Send(ctx context.Context, j Job) SendResult
}

type JobStore interface {
	Claim(ctx context.Context, channel string, limit int, lease time.Duration) ([]Job, error)
	OptedIn(ctx context.Context, customerID int64, channel string) (bool, error)
	Record(ctx context.Context, j Job, o Outcome) error
}

type Config struct {
	Channel     string
	RatePerSec  int           // 100: sözleşme sınırı
	RateMargin  float64       // 0.03: güvenlik payı, bkz. NewPacer
	Concurrency int           // 50
	BatchSize   int           // 50
	Lease       time.Duration // 30 s
	PollEvery   time.Duration // iş yokken bekleme, örn. 200 ms
	Policy      Policy
}

// Dispatcher tek bir kanalın gönderim döngüsüdür; her kanalın kendi
// limiter'ı ve semaforu var, SMS yoğunluğu email'i yavaşlatmaz.
//
// Bir tur: boş semafor slotu kadar iş claim et (beklerken lease tükenmesin),
// her işi goroutine'de işle: izin kontrolü -> limiter -> HTTP -> sonuç.
// Limiter HTTP'den hemen önce beklenir ki hız, isteğin gerçekten başladığı
// ana uygulansın. Eşzamanlılık 50: ~500 ms gecikmede 100/s için gereken
// miktar (ADR-8).
type Dispatcher struct {
	cfg    Config
	store  JobStore
	sender Sender
	pacer  *Pacer
	sem    chan struct{}
	log    *slog.Logger
	m      *Metrics
	wg     sync.WaitGroup
	// testHookAfterSend Q-06 için: provider cevabından sonra, DB'ye yazmadan
	// önce çağrılır; true dönerse "worker öldü" simüle edilir (Record yok).
	testHookAfterSend func(Job, SendResult) (crash bool)
}

func NewDispatcher(cfg Config, store JobStore, sender Sender, m *Metrics, log *slog.Logger) *Dispatcher {
	return &Dispatcher{
		cfg: cfg, store: store, sender: sender,
		pacer: NewPacer(cfg.RatePerSec, cfg.RateMargin),
		sem:   make(chan struct{}, cfg.Concurrency),
		log:   log.With("channel", cfg.Channel),
		m:     m,
	}
}

// Run ctx iptal edilene kadar döner. İptalde yeni iş claim etmez ve
// süren gönderimlerin bitmesini bekler. Bitmeyenler (süreç öldürülürse)
// lease dolunca başka bir claim ile aynı anahtarla tekrar denenir.
func (d *Dispatcher) Run(ctx context.Context) {
	defer d.wg.Wait()
	for ctx.Err() == nil {
		n, err := d.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			d.log.Warn("dispatch round failed", "err", err.Error())
		}
		if n == 0 {
			select {
			case <-ctx.Done():
			case <-time.After(d.cfg.PollEvery):
			}
		}
	}
}

// RunOnce tek bir claim turu yapar ve claim ettiği işleri başlatır (bitmesini
// beklemez). Başlatılan iş sayısını döner. Testler Wait ile bekler.
func (d *Dispatcher) RunOnce(ctx context.Context) (int, error) {
	free := cap(d.sem) - len(d.sem)
	limit := min(free, d.cfg.BatchSize)
	if limit == 0 {
		// Tüm slotlar dolu: bir slot boşalana kadar bekle, sonra tekrar dene.
		select {
		case d.sem <- struct{}{}:
			<-d.sem
		case <-ctx.Done():
		}
		return 0, nil
	}
	jobs, err := d.store.Claim(ctx, d.cfg.Channel, limit, d.cfg.Lease)
	if err != nil {
		return 0, err
	}
	for i, j := range jobs {
		select {
		case d.sem <- struct{}{}:
		case <-ctx.Done():
			// Başlatılamayan işler lease dolunca yeniden claim edilir.
			d.log.Info("shutdown with claimed jobs not started", "count", len(jobs)-i)
			return i, ctx.Err()
		}
		d.wg.Add(1)
		go func(j Job) {
			defer d.wg.Done()
			defer func() { <-d.sem }()
			d.process(ctx, j)
		}(j)
	}
	return len(jobs), nil
}

// Wait başlatılmış tüm işlerin bitmesini bekler (testler ve shutdown).
func (d *Dispatcher) Wait() { d.wg.Wait() }

// process tek bir işi uçtan uca yürütür. runCtx shutdown'da iptal olur:
// henüz başlamamış gönderim (limiter beklerken) bırakılır ve lease dolunca
// başka bir claim'le yapılır. Başlamış bir HTTP isteği ise runCtx'ten
// bağımsız (WithoutCancel) tamamlanır: yarıda kesmek sonucu belirsiz bırakır.
// Süre sınırı istemcinin kendi timeout'udur.
func (d *Dispatcher) process(runCtx context.Context, j Job) {
	ctx := context.WithoutCancel(runCtx)
	log := d.log.With("delivery_id", j.ID, "campaign_id", j.CampaignID, "customer_id", j.CustomerID, "attempt", j.Attempts)

	// 1. İzin tekrar kontrolü (AUD-02). Onaydan sonra izin kapandıysa gönderme.
	ok, err := d.store.OptedIn(ctx, j.CustomerID, j.Channel)
	if err != nil {
		// Bilinmeyen izin durumu = gönderme. Satır in_flight kalır; lease
		// dolunca tekrar denenir.
		log.Warn("opt-in check failed", "err", err.Error())
		return
	}
	if !ok {
		d.record(ctx, log, j, Outcome{Status: "skipped", LastError: "opted_out", AttemptOutcome: "skipped"}, "skipped")
		return
	}

	// 2. Hız sınırı: istek başlangıcına, yani tam burada.
	if err := d.pacer.Wait(runCtx); err != nil {
		return // shutdown: satır in_flight kalır, lease dolunca yeniden claim edilir
	}

	// 3. Gönderim.
	start := time.Now()
	res := d.sender.Send(ctx, j)
	dur := time.Since(start)
	d.m.providerDuration.WithLabelValues(j.Channel).Observe(dur.Seconds())

	if d.testHookAfterSend != nil && d.testHookAfterSend(j, res) {
		return // "worker öldü": provider kabul etmiş olabilir, DB habersiz
	}

	// 4. Karar ve kayıt.
	dec := d.cfg.Policy.Classify(res, j.Attempts)
	o := Outcome{HTTPStatus: res.HTTPStatus, Duration: dur}
	switch dec.Kind {
	case KindSent:
		o.Status, o.ProviderMessageID, o.AttemptOutcome = "sent", res.ProviderMessageID, "sent"
	case KindRetry:
		o.Status, o.Delay, o.LastError, o.AttemptOutcome = "retry_wait", dec.Delay, describe(dec, res), "retry"
		if res.Timeout {
			o.AttemptOutcome = "timeout"
		}
		d.m.retries.WithLabelValues(j.Channel, dec.Reason).Inc()
	case KindFailed:
		o.Status, o.LastError, o.AttemptOutcome = "failed", describe(dec, res), "failed"
	}
	d.record(ctx, log, j, o, dec.Reason)
}

func (d *Dispatcher) record(ctx context.Context, log *slog.Logger, j Job, o Outcome, reason string) {
	err := d.store.Record(ctx, j, o)
	switch {
	case errors.Is(err, ErrLeaseLost):
		// Başka bir worker satırı aldı; onun sonucu geçerli. Gönderim yine
		// tekti (aynı Idempotency-Key). Bilgi amaçlı log.
		log.Info("result discarded, lease lost", "status", o.Status)
		return
	case err != nil:
		// DB'ye yazılamadı: satır in_flight kalır, lease dolunca tekrar
		// claim edilir ve aynı anahtarla gönderilir (provider eski makbuzu döner).
		log.Error("record failed", "err", err.Error(), "status", o.Status)
		return
	}
	d.m.deliveries.WithLabelValues(j.Channel, o.Status).Inc()
	if o.Status != "sent" {
		log.Info("delivery attempt", "status", o.Status, "reason", reason, "http_status", o.HTTPStatus, "retry_in", o.Delay.String())
	}
}

func describe(d Decision, r SendResult) string {
	if r.HTTPStatus != 0 {
		return d.Reason + ": http " + strconv.Itoa(r.HTTPStatus)
	}
	if r.Err != nil {
		return d.Reason + ": " + r.Err.Error()
	}
	return d.Reason
}

// Metrics worker metrikleri. Etiketlerde müşteri/kampanya ID'si yok.
type Metrics struct {
	deliveries       *prometheus.CounterVec
	retries          *prometheus.CounterVec
	providerDuration *prometheus.HistogramVec
	QueueDepth       *prometheus.GaugeVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		deliveries: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "deliveries_total", Help: "Delivery attempts by final recorded status."}, []string{"channel", "outcome"}),
		retries:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "delivery_retries_total", Help: "Retries scheduled, by reason."}, []string{"channel", "reason"}),
		providerDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "provider_request_duration_seconds", Help: "Provider HTTP latency.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}}, []string{"channel"}),
		QueueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "delivery_queue_depth", Help: "Deliveries by channel and status."}, []string{"channel", "status"}),
	}
	reg.MustRegister(m.deliveries, m.retries, m.providerDuration, m.QueueDepth)
	return m
}
