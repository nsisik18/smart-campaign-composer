package audience_test

import (
	"context"
	"testing"
	"time"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
	"github.com/insider-case/smart-campaign-composer/internal/seed"
	"github.com/insider-case/smart-campaign-composer/internal/testdb"
)

var anchor = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

var example = audience.Plan{Summary: "x", Rules: []audience.Rule{
	{Field: audience.FieldCity, Op: audience.OpEq, Value: "istanbul"},
	{Field: audience.FieldCartAddedWithinDays, Op: audience.OpLte, Value: 30},
	{Field: audience.FieldPurchasedSinceLastCart, Op: audience.OpEq, Value: false},
}}

// AUD-01: izin filtresi segmentten bağımsız uygulanır; SMS izni kapalı
// hiçbir müşteri SMS alıcı listesine girmez ve sayılar Go tarafındaki
// beklentiyle birebir aynıdır.
func TestReachAndRecipientsRespectOptIn(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Apply(ctx, pool, seed.Options{Seed: seed.DefaultSeed, N: seed.DefaultN, Anchor: anchor}, false); err != nil {
		t.Fatal(err)
	}

	// Beklenen sayılar doğrudan seed üreticisinden.
	var want audience.Reach
	for _, c := range seed.Generate(seed.Options{Seed: seed.DefaultSeed, N: seed.DefaultN, Anchor: anchor}) {
		if !c.InCohort {
			continue
		}
		want.Matched++
		if c.SMSOptIn {
			want.SMSEligible++
		}
		if c.EmailOptIn {
			want.EmailEligible++
		}
		if c.SMSOptIn || c.EmailOptIn {
			want.Reachable++
		}
	}

	repo := audience.NewRepository(pool)
	got, err := repo.Count(ctx, example, anchor)
	if err != nil {
		t.Fatal(err)
	}
	if got != want || got.Matched != seed.CohortSize {
		t.Fatalf("reach=%+v want %+v", got, want)
	}
	if got.SMSEligible == got.Matched {
		t.Fatal("seed should contain cohort members without SMS opt-in, otherwise this test proves nothing")
	}

	for _, ch := range []audience.Channel{audience.ChannelSMS, audience.ChannelEmail} {
		rs, err := repo.Recipients(ctx, example, anchor, ch)
		if err != nil {
			t.Fatal(err)
		}
		col := "sms_opt_in"
		wantN := want.SMSEligible
		if ch == audience.ChannelEmail {
			col, wantN = "email_opt_in", want.EmailEligible
		}
		if len(rs) != wantN {
			t.Fatalf("%s recipients=%d want %d", ch, len(rs), wantN)
		}
		ids := make([]int64, len(rs))
		for i, r := range rs {
			ids[i] = r.CustomerID
		}
		var optedOut int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM customers WHERE id = ANY($1) AND NOT `+col, ids).Scan(&optedOut); err != nil {
			t.Fatal(err)
		}
		if optedOut != 0 {
			t.Fatalf("%s: %d recipients without opt-in", ch, optedOut)
		}
	}
}
