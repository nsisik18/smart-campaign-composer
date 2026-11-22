package seed

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Apply müşteri tablosu boşsa seed'i yazar. reseed=true ise önce tüm
// uygulama tablolarını temizler (make seed). Tek transaction: yarım seed kalmaz.
// Dönen değer yazılan satır sayısıdır; 0 ise seed atlanmıştır.
func Apply(ctx context.Context, pool *pgxpool.Pool, o Options, reseed bool) (int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// İki migrate container'ı aynı anda çalışırsa çift seed olmasın.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(724001)`); err != nil {
		return 0, err
	}
	if reseed {
		if _, err := tx.Exec(ctx,
			`TRUNCATE delivery_attempts, deliveries, campaigns, customers RESTART IDENTITY`); err != nil {
			return 0, fmt.Errorf("truncate: %w", err)
		}
	} else {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM customers`).Scan(&n); err != nil {
			return 0, err
		}
		if n > 0 {
			return 0, nil
		}
	}

	rows := Generate(o)
	src := pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
		c := rows[i]
		return []any{c.FirstName, c.LastName, c.Email, c.Phone, c.CityKey, c.Language,
			c.SMSOptIn, c.EmailOptIn, c.LastCartAt, c.LastPurchaseAt, c.TotalOrders}, nil
	})
	n, err := tx.CopyFrom(ctx, pgx.Identifier{"customers"},
		[]string{"first_name", "last_name", "email", "phone", "city_key", "language",
			"sms_opt_in", "email_opt_in", "last_cart_at", "last_purchase_at", "total_orders"}, src)
	if err != nil {
		return 0, fmt.Errorf("copy customers: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int(n), nil
}
