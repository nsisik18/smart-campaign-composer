package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
	"github.com/insider-case/smart-campaign-composer/internal/campaign"
	"github.com/insider-case/smart-campaign-composer/internal/delivery"
	"github.com/insider-case/smart-campaign-composer/internal/llm"
	"github.com/insider-case/smart-campaign-composer/internal/platform"
	"github.com/insider-case/smart-campaign-composer/internal/seed"
	"github.com/insider-case/smart-campaign-composer/internal/testdb"
)

// Pano akışı: snapshot -> periyodik stats -> terminal durumda kapanış.
func TestEventsStreamUntilCompletion(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	anchor := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	if _, err := seed.Apply(ctx, pool, seed.Options{Seed: seed.DefaultSeed, N: seed.DefaultN, Anchor: anchor}, false); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := platform.NewRegistry()
	svc := campaign.NewService(campaign.NewRepository(pool), audience.NewRepository(pool), llm.NewMock(),
		llm.NewMetrics(reg), platform.FixedClock{T: anchor}, log)
	ts := httptest.NewServer(NewRouter(Deps{Log: log, DB: pool, Registry: reg, Campaign: svc, StatsEvery: 30 * time.Millisecond}))
	defer ts.Close()

	// Bilinmeyen kampanya: stream açılmadan 404 JSON.
	resp, _ := http.Get(ts.URL + "/api/campaigns/999999/events")
	if resp.StatusCode != 404 {
		t.Fatalf("missing campaign: %d", resp.StatusCode)
	}
	resp.Body.Close()

	v, _ := svc.Create(ctx, "Son 30 günde sepete ekleyip almayan İstanbullulara %30 indirim")
	if _, err := svc.Analyze(ctx, v.Campaign.ID); err != nil {
		t.Fatal(err)
	}
	g, _ := svc.BeginGenerate(ctx, v.Campaign.ID)
	svc.RunGenerate(ctx, g, func(campaign.Event) {})
	cur, _ := svc.Get(ctx, v.Campaign.ID)
	ap, err := svc.Approve(ctx, v.Campaign.ID, cur.Campaign.Version)
	if err != nil {
		t.Fatal(err)
	}
	total := ap.Queued.SMS + ap.Queued.Email

	// Bir teslimatta 429 denemesi: panoda görünmeli.
	var did int64
	_ = pool.QueryRow(ctx, `SELECT id FROM deliveries WHERE campaign_id=$1 AND channel='sms' LIMIT 1`, v.Campaign.ID).Scan(&did)
	_, _ = pool.Exec(ctx, `INSERT INTO delivery_attempts (delivery_id, attempt_no, http_status, outcome, duration_ms) VALUES ($1, 1, 429, 'retry', 12)`, did)

	resp, err = http.Get(ts.URL + "/api/campaigns/" + itoa(v.Campaign.ID) + "/events")
	if err != nil || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("%v %s", err, resp.Header.Get("Content-Type"))
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	var events []string
	var last campaign.MonitorStats
	finished := false
	name := ""
	deadline := time.AfterFunc(10*time.Second, func() { resp.Body.Close() })
	defer deadline.Stop()
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event: ") {
			name = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var st campaign.MonitorStats
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &st); err != nil {
			t.Fatal(err)
		}
		events = append(events, name)
		last = st
		if len(events) == 1 {
			// snapshot doğru mu?
			if name != "snapshot" || st.Status != campaign.StatusSending {
				t.Fatalf("first event %s %+v", name, st)
			}
			sms := st.Channels["sms"]
			if sms.Total != ap.Queued.SMS || sms.Queued != ap.Queued.SMS || st.Channels["email"].Total != ap.Queued.Email {
				t.Fatalf("snapshot counts %+v queued %+v", st.Channels, ap.Queued)
			}
			if st.ErrorCodes["429"] != 1 || len(st.Recent) != 1 || *st.Recent[0].HTTPStatus != 429 {
				t.Fatalf("429 not visible: %+v %+v", st.ErrorCodes, st.Recent)
			}
			// Worker'ı taklit et: hepsini gönder ve kampanyayı kapat.
			_, _ = pool.Exec(ctx, `UPDATE deliveries SET status='sent', sent_at=now() WHERE campaign_id=$1`, v.Campaign.ID)
			_, _ = delivery.NewStore(pool).FinalizeCampaigns(ctx)
		}
	}
	finished = last.Terminal()
	if !finished || last.Status != campaign.StatusCompleted {
		t.Fatalf("stream ended without terminal status: %v last=%s", events, last.Status)
	}
	if got := last.Channels["sms"].Sent + last.Channels["email"].Sent; got != total {
		t.Fatalf("final sent %d want %d", got, total)
	}
	if len(events) < 2 || events[len(events)-1] != "stats" {
		t.Fatalf("events %v", events)
	}

	// Terminal kampanyaya bağlanan istemci tek snapshot alır ve akış kapanır.
	r2, _ := http.Get(ts.URL + "/api/campaigns/" + itoa(v.Campaign.ID) + "/events")
	b, _ := io.ReadAll(r2.Body)
	r2.Body.Close()
	if strings.Count(string(b), "event: ") != 1 || !strings.Contains(string(b), "event: snapshot") {
		t.Fatalf("terminal stream: %s", b)
	}
}
