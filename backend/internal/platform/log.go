package platform

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger JSON slog logger'ı döner. Kural: loglarda email/telefon yok;
// customer_id, campaign_id, kanal ve hata kodu yazılır.
func NewLogger(service, level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(h).With("service", service)
}
