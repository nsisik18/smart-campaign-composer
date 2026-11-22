package campaign_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
	"github.com/insider-case/smart-campaign-composer/internal/campaign"
	"github.com/insider-case/smart-campaign-composer/internal/llm"
	"github.com/insider-case/smart-campaign-composer/internal/platform"
	"github.com/insider-case/smart-campaign-composer/internal/seed"
	"github.com/insider-case/smart-campaign-composer/internal/testdb"
)

const exampleBrief = "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun."

// AI-01: örnek brief (mock LLM) -> geçerli segment -> gerçek Postgres'te
// garanti kohort sayısı. Segment DB'ye normalize yazılır ve geri okunur.
func TestAnalyzeExampleBriefAgainstPostgres(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	anchor := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	if _, err := seed.Apply(ctx, pool, seed.Options{Seed: seed.DefaultSeed, N: seed.DefaultN, Anchor: anchor}, false); err != nil {
		t.Fatal(err)
	}
	svc := campaign.NewService(campaign.NewRepository(pool), audience.NewRepository(pool), llm.NewMock(),
		llm.NewMetrics(prometheus.NewRegistry()), platform.FixedClock{T: anchor}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	created, err := svc.Create(ctx, exampleBrief)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.Analyze(ctx, created.Campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reach == nil || res.Reach.Matched != seed.CohortSize {
		t.Fatalf("reach %+v", res.Reach)
	}
	if res.Reach.SMSEligible >= res.Reach.Matched || res.Reach.Reachable > res.Reach.Matched {
		t.Fatalf("implausible reach %+v", res.Reach)
	}

	// Geri okuma: DB'deki JSONB tekrar validator'dan geçer ve aynı sayıyı verir.
	got, err := svc.Get(ctx, created.Campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Campaign.Status != campaign.StatusAnalyzed || got.Campaign.Version != 2 || *got.Reach != *res.Reach {
		t.Fatalf("get %+v", got)
	}

	// Yeniden analiz izinli ve versiyonu artırır.
	again, err := svc.Analyze(ctx, created.Campaign.ID)
	if err != nil || again.Campaign.Version != 3 {
		t.Fatalf("reanalyze: %v %+v", err, again.Campaign)
	}
}

// Optimistic lock: analiz sırasında kampanya değişirse yazma 409 olur.
func TestSaveSegmentVersionConflict(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	repo := campaign.NewRepository(pool)
	c, err := repo.Create(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	p := audience.Plan{Summary: "x", Rules: []audience.Rule{{Field: audience.FieldCity, Op: audience.OpEq, Value: "ankara"}}, UnsupportedCriteria: []string{}}
	if _, err := repo.SaveSegment(ctx, c.ID, c.Version, p); err != nil {
		t.Fatal(err)
	}
	var cf *campaign.ConflictError
	if _, err := repo.SaveSegment(ctx, c.ID, c.Version, p); !errors.As(err, &cf) || cf.Version != 2 {
		t.Fatalf("stale version should conflict, got %v", err)
	}
	if _, err := repo.SaveSegment(ctx, 999999, 1, p); !errors.Is(err, campaign.ErrNotFound) {
		t.Fatalf("missing campaign: %v", err)
	}
}
