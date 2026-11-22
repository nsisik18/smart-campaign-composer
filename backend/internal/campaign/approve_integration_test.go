package campaign_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
	"github.com/insider-case/smart-campaign-composer/internal/campaign"
	"github.com/insider-case/smart-campaign-composer/internal/content"
	"github.com/insider-case/smart-campaign-composer/internal/delivery"
	"github.com/insider-case/smart-campaign-composer/internal/llm"
	"github.com/insider-case/smart-campaign-composer/internal/platform"
	"github.com/insider-case/smart-campaign-composer/internal/seed"
	"github.com/insider-case/smart-campaign-composer/internal/testdb"
)

var anchor = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

// generatedCampaign: seed + create + analyze + generate (mock, geçerli içerik).
func generatedCampaign(t *testing.T) (*pgxpool.Pool, *campaign.Service, campaign.View) {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Apply(ctx, pool, seed.Options{Seed: seed.DefaultSeed, N: seed.DefaultN, Anchor: anchor}, false); err != nil {
		t.Fatal(err)
	}
	svc := campaign.NewService(campaign.NewRepository(pool), audience.NewRepository(pool), llm.NewMock(),
		llm.NewMetrics(prometheus.NewRegistry()), platform.FixedClock{T: anchor}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	v, err := svc.Create(ctx, exampleBrief)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Analyze(ctx, v.Campaign.ID); err != nil {
		t.Fatal(err)
	}
	g, err := svc.BeginGenerate(ctx, v.Campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	var last campaign.Event
	svc.RunGenerate(ctx, g, func(e campaign.Event) { last = e })
	if last.Name != "validated" {
		t.Fatalf("generation: %+v", last)
	}
	got, err := svc.Get(ctx, v.Campaign.ID)
	if err != nil || got.Campaign.Status != campaign.StatusGenerated {
		t.Fatalf("%v %+v", err, got.Campaign)
	}
	return pool, svc, got
}

func countDeliveries(t *testing.T, pool *pgxpool.Pool, id int64) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM deliveries WHERE campaign_id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestApproveSnapshotsRecipients(t *testing.T) {
	pool, svc, v := generatedCampaign(t)
	ctx := context.Background()
	id := v.Campaign.ID

	res, err := svc.Approve(ctx, id, v.Campaign.Version)
	if err != nil {
		t.Fatal(err)
	}
	if res.Campaign.Status != campaign.StatusSending || res.Campaign.Version != v.Campaign.Version+1 {
		t.Fatalf("%+v", res.Campaign)
	}
	if res.Queued.SMS != v.Reach.SMSEligible || res.Queued.Email != v.Reach.EmailEligible {
		t.Fatalf("queued %+v reach %+v", res.Queued, v.Reach)
	}
	if countDeliveries(t, pool, id) != res.Queued.SMS+res.Queued.Email {
		t.Fatal("delivery rows != queued counts")
	}

	// Her satırın anahtarı, alıcısı, render'ı ve izni doğru mu?
	rows, err := pool.Query(ctx, `
		SELECT d.customer_id, d.channel, d.recipient, d.rendered_subject, d.rendered_body, d.idempotency_key, d.status,
		       c.phone, c.email, c.first_name, c.sms_opt_in, c.email_opt_in
		FROM deliveries d JOIN customers c ON c.id = d.customer_id WHERE d.campaign_id = $1`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var cust int64
		var ch, to, body, key, st, phone, email, first string
		var subj *string
		var smsOK, emailOK bool
		if err := rows.Scan(&cust, &ch, &to, &subj, &body, &key, &st, &phone, &email, &first, &smsOK, &emailOK); err != nil {
			t.Fatal(err)
		}
		n++
		if key != delivery.IdempotencyKey(id, cust, ch) || st != "queued" {
			t.Fatalf("row %d: key/status %s %s", cust, key, st)
		}
		if strings.Contains(body, "{{") || (subj != nil && strings.Contains(*subj, "{{")) {
			t.Fatalf("unrendered placeholder for %d", cust)
		}
		if !strings.Contains(body, first) {
			t.Fatalf("body not personalised for %d", cust)
		}
		switch ch {
		case "sms":
			if to != phone || !smsOK || subj != nil || content.RuneLen(body) > content.SMSLimit {
				t.Fatalf("bad sms row for %d", cust)
			}
		case "email":
			if to != email || !emailOK || subj == nil || content.RuneLen(*subj) > content.SubjectLimit {
				t.Fatalf("bad email row for %d", cust)
			}
		}
	}
	if n == 0 {
		t.Fatal("no rows")
	}
}

// APP-01: iki (burada beş) sekmeden aynı anda onay. Tam olarak biri başarılı,
// diğerleri 409; teslimat sayısı tek onayınkiyle aynı.
func TestConcurrentApprovals(t *testing.T) {
	pool, svc, v := generatedCampaign(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, conflicts := 0, 0
	var queued campaign.QueuedCounts
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.Approve(ctx, v.Campaign.ID, v.Campaign.Version)
			mu.Lock()
			defer mu.Unlock()
			var cf *campaign.ConflictError
			switch {
			case err == nil:
				ok++
				queued = res.Queued
			case errors.As(err, &cf):
				conflicts++
				if cf.Status != campaign.StatusSending {
					t.Errorf("loser should see sending, got %s", cf.Status)
				}
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok != 1 || conflicts != 4 {
		t.Fatalf("ok=%d conflicts=%d", ok, conflicts)
	}
	if got := countDeliveries(t, pool, v.Campaign.ID); got != queued.SMS+queued.Email {
		t.Fatalf("deliveries=%d want %d", got, queued.SMS+queued.Email)
	}
	// Daha sonraki bir tekrar onay da yeni satır üretmez.
	before := countDeliveries(t, pool, v.Campaign.ID)
	if _, err := svc.Approve(ctx, v.Campaign.ID, v.Campaign.Version+1); err == nil {
		t.Fatal("re-approve must fail")
	}
	if countDeliveries(t, pool, v.Campaign.ID) != before {
		t.Fatal("re-approve changed deliveries")
	}
}

// APP-02: onaydan sonra içerik, segment ve üretim değişmez.
func TestNoChangesAfterApproval(t *testing.T) {
	_, svc, v := generatedCampaign(t)
	ctx := context.Background()
	res, err := svc.Approve(ctx, v.Campaign.ID, v.Campaign.Version)
	if err != nil {
		t.Fatal(err)
	}
	var cf *campaign.ConflictError
	edit := *v.Campaign.Content
	edit.SMSText = "Değişti {{first_name}}"
	if _, err := svc.EditContent(ctx, v.Campaign.ID, res.Campaign.Version, edit); !errors.As(err, &cf) {
		t.Fatalf("edit after approval: %v", err)
	}
	if _, err := svc.BeginGenerate(ctx, v.Campaign.ID); !errors.As(err, &cf) {
		t.Fatalf("generate after approval: %v", err)
	}
	if _, err := svc.Analyze(ctx, v.Campaign.ID); !errors.As(err, &cf) {
		t.Fatalf("analyze after approval: %v", err)
	}
	got, _ := svc.Get(ctx, v.Campaign.ID)
	if got.Campaign.Content.SMSText != v.Campaign.Content.SMSText || got.Queued == nil {
		t.Fatalf("content changed or queued missing: %+v", got)
	}
}

func TestEditContent(t *testing.T) {
	pool, svc, v := generatedCampaign(t)
	ctx := context.Background()
	id, ver := v.Campaign.ID, v.Campaign.Version

	// Geçersiz düzenleme: kaydedilmez, versiyon değişmez.
	bad := *v.Campaign.Content
	bad.SMSText = "Merhaba {{last_name}}"
	var ice *campaign.InvalidContentError
	if _, err := svc.EditContent(ctx, id, ver, bad); !errors.As(err, &ice) || ice.Issues[0].Code != "unknown_placeholder" {
		t.Fatalf("invalid edit: %v", err)
	}
	long := *v.Campaign.Content
	long.SMSText = "{{first_name}}" + strings.Repeat("a", 146) // 160 şablon, 161 render
	if _, err := svc.EditContent(ctx, id, ver, long); !errors.As(err, &ice) || ice.Issues[0].Code != "too_long" {
		t.Fatalf("too long edit: %v", err)
	}

	// Geçerli düzenleme: trim'lenmiş haliyle kaydedilir, versiyon artar.
	good := *v.Campaign.Content
	good.SMSText = "  Selam {{first_name}}, {{city}} için %30 indirim!  "
	ev, err := svc.EditContent(ctx, id, ver, good)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Campaign.Version != ver+1 || ev.Campaign.Content.SMSText != "Selam {{first_name}}, {{city}} için %30 indirim!" || ev.Checks == nil {
		t.Fatalf("%+v", ev.Campaign)
	}

	// Eski versiyonla düzenleme ve onay: 409, hiçbir şey kuyruğa girmez.
	var cf *campaign.ConflictError
	if _, err := svc.EditContent(ctx, id, ver, good); !errors.As(err, &cf) {
		t.Fatalf("stale edit: %v", err)
	}
	if _, err := svc.Approve(ctx, id, ver); !errors.As(err, &cf) || countDeliveries(t, pool, id) != 0 {
		t.Fatalf("stale approve: %v", err)
	}
	// Güncel versiyonla onay düzenlenmiş içeriği gönderir.
	if _, err := svc.Approve(ctx, id, ev.Campaign.Version); err != nil {
		t.Fatal(err)
	}
	var body string
	_ = pool.QueryRow(ctx, `SELECT rendered_body FROM deliveries WHERE campaign_id=$1 AND channel='sms' LIMIT 1`, id).Scan(&body)
	if !strings.HasPrefix(body, "Selam ") || !strings.Contains(body, "İstanbul için") {
		t.Fatalf("approved body: %q", body)
	}
}

// Onay anındaki alıcı bazlı kontrol tek bir alıcıda bile limit aşılırsa
// TÜM onayı geri alır: kısmi kuyruk yok, durum generated kalır.
// (Düzenleme yolu bunu zaten engeller; kontrolün kendisini sınamak için
// içerik doğrudan DB'ye yazılır.)
func TestApproveRollsBackOnPerRecipientViolation(t *testing.T) {
	pool, svc, v := generatedCampaign(t)
	ctx := context.Background()
	tpl := "{{first_name}}" + strings.Repeat("a", 154) // render = 154 + ad uzunluğu; 7+ harfli adlarda 160 aşılır, kısa adlarda aşılmaz
	if _, err := pool.Exec(ctx, `UPDATE campaigns SET sms_text=$2 WHERE id=$1`, v.Campaign.ID, tpl); err != nil {
		t.Fatal(err)
	}
	var ice *campaign.InvalidContentError
	if _, err := svc.Approve(ctx, v.Campaign.ID, v.Campaign.Version); !errors.As(err, &ice) || ice.Issues[0].Path != "sms_text" {
		t.Fatalf("want per-recipient violation, got %v", err)
	}
	got, _ := svc.Get(ctx, v.Campaign.ID)
	if got.Campaign.Status != campaign.StatusGenerated || countDeliveries(t, pool, v.Campaign.ID) != 0 {
		t.Fatalf("not rolled back: status=%s deliveries=%d", got.Campaign.Status, countDeliveries(t, pool, v.Campaign.ID))
	}
}
