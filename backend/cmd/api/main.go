// api: HTTP API + SSE. Kampanya akışının tamamı burada (analyze, generate,
// approve); gönderim worker'dadır.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
	"github.com/insider-case/smart-campaign-composer/internal/campaign"
	"github.com/insider-case/smart-campaign-composer/internal/httpapi"
	"github.com/insider-case/smart-campaign-composer/internal/llm"
	"github.com/insider-case/smart-campaign-composer/internal/platform"
)

func main() {
	cfg, err := platform.LoadConfig()
	log := platform.NewLogger("api", cfg.LogLevel)
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
	var provider llm.Provider
	if cfg.LLMProvider == "anthropic" {
		provider = llm.NewAnthropic(llm.AnthropicConfig{
			APIKey: cfg.AnthropicAPIKey, Model: cfg.ClaudeModel, Timeout: cfg.ClaudeTimeout, Effort: cfg.ClaudeEffort,
		})
	} else {
		mock := llm.NewMock()
		mock.ChunkDelay = 25 * time.Millisecond // demo'da streaming gözle görülsün
		provider = mock
	}
	m := llm.NewMetrics(reg)
	provider = llm.Instrument(provider, m, log)

	svc := campaign.NewService(campaign.NewRepository(pool), audience.NewRepository(pool), provider, m, platform.SystemClock{}, log)

	log.Info("starting", "llm_provider", cfg.LLMProvider, "model", cfg.ClaudeModel, "effort", cfg.ClaudeEffort)
	h := httpapi.NewRouter(httpapi.Deps{Log: log, DB: pool, Registry: reg, Campaign: svc})
	if err := platform.Serve(ctx, cfg.HTTPAddr, h, log); err != nil {
		log.Error("server failed", "err", err.Error())
		os.Exit(1)
	}
}
