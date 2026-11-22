package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/insider-case/smart-campaign-composer/internal/mockprovider"
	"github.com/insider-case/smart-campaign-composer/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fixture: bir "sending" kampanya, n müşteri (izinli) ve her biri için
// verilen kanalda bir teslimat. Onay transaction'ının ürettiği satırların
// aynısı (anahtar formatı dahil).
func fixture(t *testing.T, n int, channels ...string) (*pgxpool.Pool, int64) {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	var cid int64
	if err := pool.QueryRow(ctx, `INSERT INTO campaigns (brief, status) VALUES ('t', 'sending') RETURNING id`).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		var cust int64
		if err := pool.QueryRow(ctx, `INSERT INTO customers (first_name,last_name,email,phone,city_key,sms_opt_in,email_opt_in)
			VALUES ('Ayşe','Y',$1,$2,'istanbul',true,true) RETURNING id`,
			fmt.Sprintf("u%d@example.test", i), fmt.Sprintf("+90555%07d", i)).Scan(&cust); err != nil {
			t.Fatal(err)
		}
		for _, ch := range channels {
			var subj *string
			if ch == "email" {
				s := "Konu"
				subj = &s
			}
			if _, err := pool.Exec(ctx, `INSERT INTO deliveries (campaign_id, customer_id, channel, recipient, rendered_subject, rendered_body, idempotency_key)
				VALUES ($1,$2,$3,$4,$5,'Merhaba Ayşe',$6)`, cid, cust, ch, fmt.Sprintf("to-%d-%s", cust, ch), subj, IdempotencyKey(cid, cust, ch)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return pool, cid
}

func testPolicy(max int) Policy {
	return Policy{MaxAttempts: max, Base: time.Second, Cap: 30 * time.Second, Rand: func() float64 { return 0 }} // backoff 0: test beklemez
}

func newDispatcher(t *testing.T, pool *pgxpool.Pool, channel string, sender Sender, max int) *Dispatcher {
	t.Helper()
	return NewDispatcher(Config{Channel: channel, RatePerSec: 1000, RateMargin: 0, Concurrency: 20, BatchSize: 50, Lease: 30 * time.Second,
		PollEvery: 10 * time.Millisecond, Policy: testPolicy(max)}, NewStore(pool), sender, NewMetrics(prometheus.NewRegistry()), quiet)
}

// drain terminal olmayan teslimat kalmayana kadar tur atar.
func drain(t *testing.T, d *Dispatcher, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := d.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		d.Wait()
		var open int
		_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM deliveries WHERE channel = $1 AND status IN ('queued','in_flight','retry_wait')`, d.cfg.Channel).Scan(&open)
		if open == 0 {
			return
		}
		// retry_wait'teki Retry-After'lı satırları beklemeden öne çek.
		_, _ = pool.Exec(context.Background(), `UPDATE deliveries SET next_attempt_at = now() WHERE status = 'retry_wait'`)
	}
	t.Fatal("drain timed out")
}

type row struct {
	Status, LastError string
	Attempts          int
	ProviderID        *string
}

func getRow(t *testing.T, pool *pgxpool.Pool, id int64) row {
	t.Helper()
	var r row
	var le *string
	if err := pool.QueryRow(context.Background(), `SELECT status, attempts, provider_message_id, last_error FROM deliveries WHERE id=$1`, id).
		Scan(&r.Status, &r.Attempts, &r.ProviderID, &le); err != nil {
		t.Fatal(err)
	}
	if le != nil {
		r.LastError = *le
	}
	return r
}

func attempts(t *testing.T, pool *pgxpool.Pool, id int64) []string {
	t.Helper()
	rows, _ := pool.Query(context.Background(), `SELECT outcome || ':' || COALESCE(http_status::text,'-') FROM delivery_attempts WHERE delivery_id=$1 ORDER BY attempt_no`, id)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func onlyDelivery(t *testing.T, pool *pgxpool.Pool) int64 {
	var id int64
	_ = pool.QueryRow(context.Background(), `SELECT id FROM deliveries ORDER BY id LIMIT 1`).Scan(&id)
	return id
}

// scripted belirli bir anahtar için çağrı sırasına göre cevap veren sahte provider.
type scripted struct {
	mu    sync.Mutex
	calls map[string]int
	plan  func(call int) (status int, retryAfter string)
}

func (s *scripted) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		s.mu.Lock()
		s.calls[key]++
		n := s.calls[key]
		s.mu.Unlock()
		code, ra := s.plan(n)
		if ra != "" {
			w.Header().Set("Retry-After", ra)
		}
		w.WriteHeader(code)
		if code/100 == 2 {
			_ = json.NewEncoder(w).Encode(map[string]string{"provider_message_id": "pm_" + key[:6]})
		}
	})
}

func scriptedSender(t *testing.T, plan func(int) (int, string)) (*Client, *scripted) {
	s := &scripted{calls: map[string]int{}, plan: plan}
	ts := httptest.NewServer(s.handler())
	t.Cleanup(ts.Close)
	return NewClient(ts.URL, 2*time.Second), s
}

// Q-01: iki worker aynı anda claim eder; hiçbir satır iki kez alınmaz.
func TestConcurrentClaimsAreDisjoint(t *testing.T) {
	pool, _ := fixture(t, 300, "sms")
	st := NewStore(pool)
	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				jobs, err := st.Claim(context.Background(), "sms", 25, 30*time.Second)
				if err != nil {
					t.Error(err)
					return
				}
				if len(jobs) == 0 {
					return
				}
				mu.Lock()
				for _, j := range jobs {
					seen[j.ID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != 300 {
		t.Fatalf("claimed %d distinct rows, want 300", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("row %d claimed %d times", id, n)
		}
	}
	// Lease'i dolmamış satır tekrar claim edilmez; email kanalı etkilenmez.
	if jobs, _ := st.Claim(context.Background(), "sms", 50, 30*time.Second); len(jobs) != 0 {
		t.Fatalf("re-claimed %d leased rows", len(jobs))
	}
}

// Q-02: 429 + Retry-After -> retry_wait, next_attempt_at Retry-After'a uyar; sonra sent.
func TestRetryAfterThenSent(t *testing.T) {
	pool, _ := fixture(t, 1, "sms")
	client, prov := scriptedSender(t, func(call int) (int, string) {
		if call == 1 {
			return 429, "2"
		}
		return 202, ""
	})
	d := newDispatcher(t, pool, "sms", client, 5)
	id := onlyDelivery(t, pool)

	_, _ = d.RunOnce(context.Background())
	d.Wait()
	r := getRow(t, pool, id)
	var wait float64
	_ = pool.QueryRow(context.Background(), `SELECT EXTRACT(EPOCH FROM next_attempt_at - updated_at) FROM deliveries WHERE id=$1`, id).Scan(&wait)
	if r.Status != "retry_wait" || wait < 1.9 || wait > 2.1 || r.LastError != "rate_limited: http 429" {
		t.Fatalf("after 429: %+v wait=%.2f", r, wait)
	}
	// Zamanı gelmeden claim edilmez.
	if n, _ := d.RunOnce(context.Background()); n != 0 {
		t.Fatal("claimed before Retry-After elapsed")
	}
	drain(t, d, pool)
	r = getRow(t, pool, id)
	if r.Status != "sent" || r.Attempts != 2 || r.ProviderID == nil {
		t.Fatalf("final %+v", r)
	}
	if got := attempts(t, pool, id); fmt.Sprint(got) != "[retry:429 sent:202]" {
		t.Fatalf("attempts %v", got)
	}
	if len(prov.calls) != 1 {
		t.Fatal("all attempts must use the same idempotency key")
	}
}

// Q-03: 503 ve timeout -> backoff ile tekrar, deneme kayıtları.
func TestServerErrorAndTimeoutRetried(t *testing.T) {
	pool, _ := fixture(t, 1, "sms")
	slow := make(chan struct{})
	s := &scripted{calls: map[string]int{}}
	s.plan = func(call int) (int, string) {
		switch call {
		case 1:
			return 503, ""
		case 2:
			<-slow // istemci timeout'una kadar cevap verme
			return 202, ""
		}
		return 202, ""
	}
	ts := httptest.NewServer(s.handler())
	defer ts.Close()
	defer close(slow)
	client := NewClient(ts.URL, 200*time.Millisecond)

	d := newDispatcher(t, pool, "sms", client, 5)
	id := onlyDelivery(t, pool)
	drain(t, d, pool)
	r := getRow(t, pool, id)
	if r.Status != "sent" || r.Attempts != 3 {
		t.Fatalf("%+v", r)
	}
	if got := attempts(t, pool, id); fmt.Sprint(got) != "[retry:503 timeout:- sent:202]" {
		t.Fatalf("attempts %v", got)
	}
}

// Q-04: 422 kalıcıdır: failed, bir daha denenmez.
func TestPermanentFailureNotRetried(t *testing.T) {
	pool, _ := fixture(t, 1, "sms")
	client, prov := scriptedSender(t, func(int) (int, string) { return 422, "" })
	d := newDispatcher(t, pool, "sms", client, 5)
	id := onlyDelivery(t, pool)
	drain(t, d, pool)
	if n, _ := d.RunOnce(context.Background()); n != 0 {
		t.Fatal("failed row claimed again")
	}
	d.Wait()
	r := getRow(t, pool, id)
	var calls int
	for _, c := range prov.calls {
		calls += c
	}
	if r.Status != "failed" || r.Attempts != 1 || calls != 1 || r.LastError != "client_error: http 422" {
		t.Fatalf("%+v calls=%d", r, calls)
	}
}

// Q-05: deneme hakkı biter -> failed, last_error dolu.
func TestMaxAttemptsExhausted(t *testing.T) {
	pool, _ := fixture(t, 1, "sms")
	client, prov := scriptedSender(t, func(int) (int, string) { return 503, "" })
	d := newDispatcher(t, pool, "sms", client, 5)
	id := onlyDelivery(t, pool)
	drain(t, d, pool)
	r := getRow(t, pool, id)
	var calls int
	for _, c := range prov.calls {
		calls += c
	}
	if r.Status != "failed" || r.Attempts != 5 || calls != 5 || r.LastError != "max_attempts: http 503" {
		t.Fatalf("%+v calls=%d", r, calls)
	}
}

func providerStats(t *testing.T, url string) map[string]any {
	resp, err := http.Get(url + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return m
}

// Q-06: provider kabul etti, worker DB'ye yazamadan öldü.
//
//  1. Worker A claim eder (attempts=1), gönderir, provider KABUL eder,
//     A ölür (Record yok). Satır in_flight, lease süresi içinde.
//  2. Lease dolmadan kimse claim edemez.
//  3. Lease dolar. Worker B claim eder (attempts=2), AYNI anahtarla gönderir.
//     Provider yeni gönderim yapmaz, eski makbuzu (aynı id) döner.
//  4. Sonuç: çağrı 2, kabul 1, durum sent, provider_message_id = ilk makbuz.
//  5. A geç dirilip eski sonucu yazmaya kalkarsa fencing reddeder.
func TestProviderAcceptedWorkerDied(t *testing.T) {
	pool, _ := fixture(t, 1, "sms")
	prov := mockprovider.NewServer("none", quiet, mockprovider.WithLatency(func() time.Duration { return 0 }))
	ts := httptest.NewServer(prov.Routes())
	defer ts.Close()
	client := NewClient(ts.URL, 2*time.Second)
	id := onlyDelivery(t, pool)

	// 1. A gönderir ve "ölür".
	a := newDispatcher(t, pool, "sms", client, 5)
	var crashed Job
	var firstReceipt string
	a.testHookAfterSend = func(j Job, r SendResult) bool {
		crashed, firstReceipt = j, r.ProviderMessageID
		return true
	}
	_, _ = a.RunOnce(context.Background())
	a.Wait()
	if st := providerStats(t, ts.URL); st["accepted"].(float64) != 1 || firstReceipt == "" {
		t.Fatalf("provider should have accepted once: %v", st)
	}
	if r := getRow(t, pool, id); r.Status != "in_flight" || r.Attempts != 1 {
		t.Fatalf("after crash: %+v", r)
	}

	// 2. Lease geçerliyken B alamaz.
	b := newDispatcher(t, pool, "sms", client, 5)
	if n, _ := b.RunOnce(context.Background()); n != 0 {
		t.Fatal("claimed a row with a live lease")
	}

	// 3. Lease dolar (30 s beklemek yerine saati ileri alıyoruz).
	if _, err := pool.Exec(context.Background(), `UPDATE deliveries SET lease_until = now() - interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if n, _ := b.RunOnce(context.Background()); n != 1 {
		t.Fatal("expired lease should be reclaimable")
	}
	b.Wait()

	// 4. Tek gönderim.
	r := getRow(t, pool, id)
	st := providerStats(t, ts.URL)
	if r.Status != "sent" || r.Attempts != 2 || r.ProviderID == nil || *r.ProviderID != firstReceipt {
		t.Fatalf("final %+v first=%s", r, firstReceipt)
	}
	if st["accepted"].(float64) != 1 || st["total_calls"].(float64) != 2 || st["max_calls_per_key"].(float64) != 2 {
		t.Fatalf("provider stats %v", st)
	}
	if got := attempts(t, pool, id); fmt.Sprint(got) != "[sent:200]" {
		t.Fatalf("only the surviving attempt is recorded: %v", got)
	}

	// 5. A'nın geç gelen sonucu yazılamaz.
	err := NewStore(pool).Record(context.Background(), crashed, Outcome{Status: "failed", LastError: "late", AttemptOutcome: "failed"})
	if err != ErrLeaseLost {
		t.Fatalf("stale owner write: %v", err)
	}
	if r2 := getRow(t, pool, id); r2.Status != "sent" {
		t.Fatalf("stale owner overwrote: %+v", r2)
	}
}

// AUD-02: onaydan sonra izin kapanırsa teslimat skipped; provider çağrılmaz.
func TestOptOutAfterApprovalIsSkipped(t *testing.T) {
	pool, _ := fixture(t, 3, "sms", "email")
	ctx := context.Background()
	var victim int64
	_ = pool.QueryRow(ctx, `SELECT id FROM customers ORDER BY id LIMIT 1`).Scan(&victim)
	_, _ = pool.Exec(ctx, `UPDATE customers SET sms_opt_in = false WHERE id = $1`, victim)

	client, prov := scriptedSender(t, func(int) (int, string) { return 202, "" })
	drain(t, newDispatcher(t, pool, "sms", client, 5), pool) // yalnızca sms kanalı
	var st string
	_ = pool.QueryRow(ctx, `SELECT status FROM deliveries WHERE customer_id=$1 AND channel='sms'`, victim).Scan(&st)
	if st != "skipped" {
		t.Fatalf("opted-out sms delivery: %s", st)
	}
	if prov.calls[IdempotencyKey(1, victim, "sms")] != 0 || len(prov.calls) != 2 {
		t.Fatalf("provider calls %v", prov.calls)
	}
	// Email izni hâlâ açık: email teslimatı etkilenmedi.
	_ = pool.QueryRow(ctx, `SELECT status FROM deliveries WHERE customer_id=$1 AND channel='email'`, victim).Scan(&st)
	if st != "queued" {
		t.Fatalf("email should be untouched by sms opt-out: %s", st)
	}
}

func TestFinalizeCampaigns(t *testing.T) {
	pool, cid := fixture(t, 2, "sms")
	st := NewStore(pool)
	ctx := context.Background()
	if ids, _ := st.FinalizeCampaigns(ctx); len(ids) != 0 {
		t.Fatal("open deliveries: must not finalize")
	}
	_, _ = pool.Exec(ctx, `UPDATE deliveries SET status='sent'`)
	if ids, _ := st.FinalizeCampaigns(ctx); len(ids) != 1 || ids[0] != cid {
		t.Fatalf("finalize %v", ids)
	}
	var s string
	_ = pool.QueryRow(ctx, `SELECT status FROM campaigns WHERE id=$1`, cid).Scan(&s)
	if s != "completed" {
		t.Fatal(s)
	}
	if ids, _ := st.FinalizeCampaigns(ctx); len(ids) != 0 {
		t.Fatal("finalize must be idempotent")
	}

	pool2, cid2 := fixture(t, 2, "sms")
	_, _ = pool2.Exec(ctx, `UPDATE deliveries SET status='sent'`)
	_, _ = pool2.Exec(ctx, `UPDATE deliveries SET status='failed' WHERE id=(SELECT min(id) FROM deliveries)`)
	_, _ = NewStore(pool2).FinalizeCampaigns(ctx)
	_ = pool2.QueryRow(ctx, `SELECT status FROM campaigns WHERE id=$1`, cid2).Scan(&s)
	if s != "completed_with_errors" {
		t.Fatal(s)
	}
}
