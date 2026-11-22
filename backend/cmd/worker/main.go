// worker: deliveries tablosundan iş alıp provider'a gönderir.
//
// Kanal başına bağımsız bir delivery.Dispatcher çalışır (kendi limiter'ı ve
// semaforu). Ayrıca:
//   - finalizer (1 s): terminal olmayan teslimatı kalmamış kampanyaları kapatır;
//   - queue depth (2 s): delivery_queue_depth gauge'unu günceller;
//   - health/metrics HTTP sunucusu (:8081).
//
// Tek replica varsayımı (DECISIONS): hız sınırı süreç içidir. İki worker
// çalıştırmak claim açısından GÜVENLİDİR (SKIP LOCKED + lease + fencing),
// ama kanal başına toplam hız 2x olur; çoklu replica için dağıtık limiter gerekir.
package main

import (
	"context"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/insider-case/smart-campaign-composer/internal/delivery"
	"github.com/insider-case/smart-campaign-composer/internal/platform"
)

func main() {
	cfg, err := platform.LoadConfig()
	log := platform.NewLogger("worker", cfg.LogLevel)
	if err != nil {
		log.Error("invalid config", "err", err.Error())
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := platform.ConnectDB(ctx, cfg.DatabaseURL, log)
	if err != nil {
		log.Error("db connect failed", "err", err.Error())
		os.Exit(1)
	}
	defer pool.Close()

	reg := platform.NewRegistry()
	m := delivery.NewMetrics(reg)
	store := delivery.NewStore(pool)
	client := delivery.NewClient(cfg.ProviderURL, cfg.ProviderHTTPTimeout)

	mux := http.NewServeMux()
	mux.Handle("GET /health/live", platform.LiveHandler())
	mux.Handle("GET /health/ready", platform.ReadyHandler(
		platform.Check{Name: "db", Fn: pool.Ping},
		platform.HTTPCheck("provider", cfg.ProviderURL+"/health"),
	))
	mux.Handle("GET /metrics", platform.MetricsHandler(reg))
	go func() {
		if err := platform.Serve(ctx, cfg.WorkerHTTPAddr, mux, log); err != nil {
			log.Error("worker http failed", "err", err.Error())
			stop()
		}
	}()

	policy := delivery.Policy{MaxAttempts: cfg.MaxAttempts, Base: time.Second, Cap: 30 * time.Second, Rand: rand.Float64}
	var wg sync.WaitGroup
	for _, ch := range []string{"sms", "email"} {
		d := delivery.NewDispatcher(delivery.Config{
			Channel: ch, RatePerSec: cfg.RatePerChannel, RateMargin: cfg.RateMargin,
			Concurrency: cfg.ConcurrencyPerChan, BatchSize: 50, Lease: cfg.LeaseDuration,
			PollEvery: 200 * time.Millisecond, Policy: policy,
		}, store, client, m, log)
		wg.Add(1)
		go func() { defer wg.Done(); d.Run(ctx) }()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		every(ctx, time.Second, func() {
			ids, err := store.FinalizeCampaigns(ctx)
			if err != nil {
				log.Warn("finalize failed", "err", err.Error())
			}
			for _, id := range ids {
				log.Info("campaign completed", "campaign_id", id)
			}
		})
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		every(ctx, 2*time.Second, func() {
			depth, err := store.QueueDepth(ctx)
			if err != nil {
				return
			}
			m.QueueDepth.Reset()
			for k, n := range depth {
				m.QueueDepth.WithLabelValues(k[0], k[1]).Set(float64(n))
			}
		})
	}()

	log.Info("worker started",
		"rate_per_channel", cfg.RatePerChannel, "rate_margin", cfg.RateMargin,
		"concurrency_per_channel", cfg.ConcurrencyPerChan, "max_attempts", cfg.MaxAttempts,
		"lease", cfg.LeaseDuration.String(), "provider_timeout", cfg.ProviderHTTPTimeout.String())
	<-ctx.Done()
	log.Info("worker stopping: waiting for in-flight sends")
	wg.Wait()
}

func every(ctx context.Context, d time.Duration, f func()) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f()
		}
	}
}
