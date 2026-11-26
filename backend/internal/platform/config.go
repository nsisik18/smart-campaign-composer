// Package platform tüm binary'lerin paylaştığı altyapı parçalarını içerir:
// config, DB bağlantısı, log, metrics, health. İş kuralı içermez.
package platform

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config env'den okunur. Her binary yalnızca ihtiyacı olan alanları kullanır.
type Config struct {
	DatabaseURL string
	LogLevel    string

	// api
	HTTPAddr        string
	LLMProvider     string // mock | anthropic
	AnthropicAPIKey string
	ClaudeModel     string
	ClaudeTimeout   time.Duration
	ClaudeEffort    string

	// worker
	WorkerHTTPAddr      string
	ProviderURL         string
	RatePerChannel      int
	RateMargin          float64
	ConcurrencyPerChan  int
	MaxAttempts         int
	ProviderHTTPTimeout time.Duration
	LeaseDuration       time.Duration

	// mock-provider
	MockProviderAddr string
	MockProfile      string // none | deterministic
}

func LoadConfig() (Config, error) {
	var errs []error
	c := Config{
		DatabaseURL:         env("DATABASE_URL", "postgres://app:app@localhost:5432/campaigns?sslmode=disable"),
		LogLevel:            env("LOG_LEVEL", "info"),
		HTTPAddr:            env("HTTP_ADDR", ":8080"),
		LLMProvider:         os.Getenv("LLM_PROVIDER"),
		AnthropicAPIKey:     os.Getenv("ANTHROPIC_API_KEY"),
		ClaudeModel:         env("CLAUDE_MODEL", "claude-sonnet-5-5"),
		ClaudeTimeout:       envSeconds("CLAUDE_TIMEOUT_SECONDS", 30, &errs),
		ClaudeEffort:        env("CLAUDE_EFFORT", "low"),
		WorkerHTTPAddr:      env("WORKER_HTTP_ADDR", ":8081"),
		ProviderURL:         env("PROVIDER_URL", "http://localhost:8090"),
		RatePerChannel:      envInt("DELIVERY_RATE_PER_CHANNEL", 100, &errs),
		RateMargin:          envFloat("DELIVERY_RATE_MARGIN", 0.03, &errs),
		ConcurrencyPerChan:  envInt("DELIVERY_CONCURRENCY_PER_CHANNEL", 50, &errs),
		MaxAttempts:         envInt("DELIVERY_MAX_ATTEMPTS", 5, &errs),
		ProviderHTTPTimeout: envSeconds("DELIVERY_HTTP_TIMEOUT_SECONDS", 5, &errs),
		LeaseDuration:       envSeconds("DELIVERY_LEASE_SECONDS", 30, &errs),
		MockProviderAddr:    env("MOCK_PROVIDER_ADDR", ":8090"),
		MockProfile:         env("MOCK_PROFILE", "deterministic"),
	}
	// Boşsa key'e göre seç: key varsa gerçek Claude, yoksa mock.
	if c.LLMProvider == "" {
		c.LLMProvider = "mock"
		if c.AnthropicAPIKey != "" {
			c.LLMProvider = "anthropic"
		}
	}
	if c.LLMProvider != "mock" && c.LLMProvider != "anthropic" {
		errs = append(errs, fmt.Errorf("LLM_PROVIDER must be mock|anthropic, got %q", c.LLMProvider))
	}
	if c.LLMProvider == "anthropic" && c.AnthropicAPIKey == "" {
		errs = append(errs, fmt.Errorf("LLM_PROVIDER=anthropic requires ANTHROPIC_API_KEY"))
	}
	// Lease, provider timeout'undan belirgin uzun olmalı; aksi halde hâlâ
	// süren bir HTTP çağrısının satırı başka bir claim'e kaptırılır.
	if c.LeaseDuration < 3*c.ProviderHTTPTimeout {
		errs = append(errs, fmt.Errorf("DELIVERY_LEASE_SECONDS (%s) must be >= 3x DELIVERY_HTTP_TIMEOUT_SECONDS (%s)",
			c.LeaseDuration, c.ProviderHTTPTimeout))
	}
	if len(errs) > 0 {
		return c, fmt.Errorf("config: %v", errs)
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int, errs *[]error) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be a positive integer, got %q", key, v))
		return def
	}
	return n
}

func envFloat(key string, def float64, errs *[]error) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 || f >= 0.5 {
		*errs = append(*errs, fmt.Errorf("%s must be in [0, 0.5), got %q", key, v))
		return def
	}
	return f
}

func envSeconds(key string, def int, errs *[]error) time.Duration {
	return time.Duration(envInt(key, def, errs)) * time.Second
}
