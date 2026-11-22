// Package testdb integration testleri için izole Postgres veritabanı açar.
//
// Her çağrı TEST_DATABASE_URL sunucusunda rastgele adlı yeni bir veritabanı
// oluşturur, migration'ları uygular ve test bitince siler. Böylece `go test`
// paketleri paralel çalışırken birbirinin verisini truncate etmez.
// TEST_DATABASE_URL yoksa test atlanır.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/insider-case/smart-campaign-composer/internal/dbmigrate"
	"github.com/insider-case/smart-campaign-composer/internal/platform"
)

func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "t_" + hex.EncodeToString(b)

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("testdb: connect admin: %v", err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("testdb: create: %v", err)
	}
	conn.Close(ctx)

	u, _ := url.Parse(admin)
	u.Path = "/" + name
	dsn := u.String()
	if err := dbmigrate.Up(ctx, dsn); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}
	pool, err := platform.ConnectDB(ctx, dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("testdb: pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, err := pgx.Connect(context.Background(), admin)
		if err != nil {
			return
		}
		defer c.Close(context.Background())
		_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return pool
}
