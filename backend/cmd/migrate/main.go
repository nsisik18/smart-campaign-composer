// migrate: compose'ta bir kez çalışıp çıkar. Migration'ları uygular, müşteri
// tablosu boşsa seed'i yazar. `-reseed` tüm uygulama verisini silip baştan
// seed'ler (make seed).
package main

import (
	"context"
	"flag"
	"os"
	"time"

	"github.com/insider-case/smart-campaign-composer/internal/dbmigrate"
	"github.com/insider-case/smart-campaign-composer/internal/platform"
	"github.com/insider-case/smart-campaign-composer/internal/seed"
)

func main() {
	reseed := flag.Bool("reseed", false, "truncate app tables and seed again")
	flag.Parse()

	cfg, _ := platform.LoadConfig() // yalnızca DATABASE_URL ve LOG_LEVEL kullanılır
	log := platform.NewLogger("migrate", cfg.LogLevel)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := platform.ConnectDB(ctx, cfg.DatabaseURL, log) // DB hazır olana kadar bekler
	if err != nil {
		log.Error("db connect failed", "err", err.Error())
		os.Exit(1)
	}
	defer pool.Close()

	if err := dbmigrate.Up(ctx, cfg.DatabaseURL); err != nil {
		log.Error("migration failed", "err", err.Error())
		os.Exit(1)
	}
	log.Info("migrations applied")

	n, err := seed.Apply(ctx, pool, seed.Options{Seed: seed.DefaultSeed, N: seed.DefaultN, Anchor: time.Now().UTC()}, *reseed)
	if err != nil {
		log.Error("seed failed", "err", err.Error())
		os.Exit(1)
	}
	if n == 0 {
		log.Info("seed skipped: customers already present")
	} else {
		log.Info("seed applied", "customers", n, "guaranteed_cohort", seed.CohortSize)
	}
}
