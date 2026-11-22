package httpapi

import (
	"bufio"
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
	"github.com/insider-case/smart-campaign-composer/internal/llm"
	"github.com/insider-case/smart-campaign-composer/internal/platform"
	"github.com/insider-case/smart-campaign-composer/internal/seed"
	"github.com/insider-case/smart-campaign-composer/internal/testdb"
)

type sseEvent struct {
	name string
	data map[string]any
}

func readSSE(t *testing.T, body io.Reader) []sseEvent {
	t.Helper()
	var out []sseEvent
	sc := bufio.NewScanner(body)
	var name string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var d map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &d); err != nil {
				t.Fatalf("bad data line %q: %v", line, err)
			}
			out = append(out, sseEvent{name, d})
		}
	}
	return out
}

// Uçtan uca: HTTP -> servis -> mock LLM stream -> parser -> SSE -> Postgres.
func TestGenerateSSEEndToEnd(t *testing.T) {
	pool := testdb.New(t)
	ctx := t.Context()
	anchor := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	if _, err := seed.Apply(ctx, pool, seed.Options{Seed: seed.DefaultSeed, N: seed.DefaultN, Anchor: anchor}, false); err != nil {
		t.Fatal(err)
	}
	reg := platform.NewRegistry()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := campaign.NewService(campaign.NewRepository(pool), audience.NewRepository(pool), llm.NewMock(),
		llm.NewMetrics(reg), platform.FixedClock{T: anchor}, log)
	ts := httptest.NewServer(NewRouter(Deps{Log: log, DB: pool, Registry: reg, Campaign: svc}))
	defer ts.Close()

	post := func(path, body string) *http.Response {
		resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := post("/api/campaigns", `{"brief":"Son 30 günde sepete ekleyip almayan İstanbullulara %30 indirim"}`)
	var created campaign.View
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	id := itoa(created.Campaign.ID)

	// Analizden önce üretim: stream açılmadan 409 JSON.
	resp = post("/api/campaigns/"+id+"/generate", "")
	if resp.StatusCode != http.StatusConflict || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("pre-analysis generate: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	resp.Body.Close()

	post("/api/campaigns/"+id+"/analyze", "").Body.Close()

	resp = post("/api/campaigns/"+id+"/generate", "")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("generate: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	events := readSSE(t, resp.Body)
	resp.Body.Close()
	if len(events) < 3 || events[0].name != "started" || events[len(events)-1].name != "validated" {
		t.Fatalf("events: first=%v last=%v n=%d", events[0], events[len(events)-1], len(events))
	}
	sms := ""
	for _, e := range events {
		if e.name == "delta" && e.data["field"] == "sms_text" {
			sms += e.data["text"].(string)
		}
	}

	// GET kayıtlı içeriği ve uzunluk kontrollerini döner; stream ile aynı.
	r2, _ := http.Get(ts.URL + "/api/campaigns/" + id)
	var view campaign.View
	_ = json.NewDecoder(r2.Body).Decode(&view)
	r2.Body.Close()
	if view.Campaign.Status != campaign.StatusGenerated || view.Campaign.Content == nil || view.Campaign.Content.SMSText != sms {
		t.Fatalf("view %+v", view.Campaign)
	}
	if view.Checks == nil || !view.Checks.SMS.OK || view.Checks.PreviewVars.City != "İstanbul" {
		t.Fatalf("checks %+v", view.Checks)
	}
	// Seed'deki en uzun ad (15 karakter) render sınırında kullanıldı.
	if n := len([]rune(view.Checks.PreviewVars.FirstName)); n != 15 {
		t.Fatalf("longest first name not used: %q", view.Checks.PreviewVars.FirstName)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
