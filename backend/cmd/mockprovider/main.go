// mock-provider: bağımsız sahte SMS/email sağlayıcısı (BLUEPRINT §7).
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/insider-case/smart-campaign-composer/internal/mockprovider"
	"github.com/insider-case/smart-campaign-composer/internal/platform"
)

func main() {
	cfg, _ := platform.LoadConfig() // provider yalnızca addr/profil okur; diğer alan hataları onu ilgilendirmez
	log := platform.NewLogger("mock-provider", cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := mockprovider.NewServer(cfg.MockProfile, log)
	if err := platform.Serve(ctx, cfg.MockProviderAddr, srv.Routes(), log); err != nil {
		log.Error("server failed", "err", err.Error())
		os.Exit(1)
	}
}
