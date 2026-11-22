package seed

import (
	"context"
	"testing"

	"github.com/insider-case/smart-campaign-composer/internal/testdb"
)

func TestApplySeedAgainstPostgres(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	opts := Options{Seed: DefaultSeed, N: DefaultN, Anchor: anchor}
	n, err := Apply(ctx, pool, opts, false)
	if err != nil || n != DefaultN {
		t.Fatalf("first apply: n=%d err=%v", n, err)
	}
	// İkinci çağrı idempotent olmalı.
	if n, err := Apply(ctx, pool, opts, false); err != nil || n != 0 {
		t.Fatalf("second apply should skip: n=%d err=%v", n, err)
	}
	// reseed baştan yazar, sayı değişmez.
	if n, err := Apply(ctx, pool, opts, true); err != nil || n != DefaultN {
		t.Fatalf("reseed: n=%d err=%v", n, err)
	}
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM customers`).Scan(&total); err != nil || total != DefaultN {
		t.Fatalf("total=%d err=%v", total, err)
	}
}
