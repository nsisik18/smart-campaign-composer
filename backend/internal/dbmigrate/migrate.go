// Package dbmigrate gömülü goose migration'larını çalıştırır. migrate
// binary'si ve integration testleri aynı kodu kullanır.
package dbmigrate

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/insider-case/smart-campaign-composer/migrations"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql sürücüsü "pgx"
	"github.com/pressly/goose/v3"
)

func Up(ctx context.Context, databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
