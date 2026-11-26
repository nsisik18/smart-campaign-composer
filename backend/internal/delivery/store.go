package delivery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
)

// Job claim edilmiş, şu an bu worker'ın lease'inde olan bir teslimat.
// Attempts, claim'in artırdığı değerdir ve FENCING TOKEN olarak kullanılır:
// sonucu yazarken "attempts hâlâ benimki mi" diye bakılır.
type Job struct {
	ID         int64
	CampaignID int64
	CustomerID int64
	Channel    string
	Recipient  string
	Subject    *string
	Body       string
	Key        string
	Attempts   int
}

type Store struct{ db *pgxpool.Pool }

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

// Claim bir kanal için en fazla limit iş alır ve lease'ler.
//
//   - Uygun satır: (queued|retry_wait ve zamanı gelmiş) VEYA (in_flight ve
//     lease'i dolmuş: sahibi öldü ya da takıldı).
//   - FOR UPDATE SKIP LOCKED: eşzamanlı iki claim aynı satırı kilitlemeye
//     çalışmaz; biri diğerinin kilitlediklerini atlar (Q-01). Bekleme yok.
//   - Tek UPDATE ... FROM (CTE) ifadesi kendi başına bir transaction'dır ve
//     kısadır; HTTP çağrısı sırasında hiçbir transaction açık TUTULMAZ.
//     Sahiplik kilitle değil lease_until ile ifade edilir.
//   - attempts burada artırılır: deneme "başladı" sayılır. Worker gönderip
//     ölürse bile bu deneme sayılmış olur; sonsuz yeniden claim döngüsü
//     MaxAttempts ile sınırlıdır.
//   - Zamanlar DB saatiyle (now()) hesaplanır: worker ile DB arasında saat
//     kayması lease mantığını bozmaz.
func (s *Store) Claim(ctx context.Context, channel string, limit int, lease time.Duration) ([]Job, error) {
	rows, err := s.db.Query(ctx, `
		WITH c AS (
		  SELECT id FROM deliveries
		  WHERE channel = $1
		    AND ( (status IN ('queued','retry_wait') AND next_attempt_at <= now())
		       OR (status = 'in_flight' AND lease_until < now()) )
		  ORDER BY next_attempt_at, id
		  LIMIT $2
		  FOR UPDATE SKIP LOCKED
		)
		UPDATE deliveries d
		SET status = 'in_flight', attempts = d.attempts + 1,
		    lease_until = now() + make_interval(secs => $3), updated_at = now()
		FROM c WHERE d.id = c.id
		RETURNING d.id, d.campaign_id, d.customer_id, d.channel, d.recipient,
		          d.rendered_subject, d.rendered_body, d.idempotency_key, d.attempts`,
		channel, limit, lease.Seconds())
	if err != nil {
		return nil, fmt.Errorf("claim: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Job, error) {
		var j Job
		err := r.Scan(&j.ID, &j.CampaignID, &j.CustomerID, &j.Channel, &j.Recipient, &j.Subject, &j.Body, &j.Key, &j.Attempts)
		return j, err
	})
}

// OptedIn müşterinin kanal iznini GÖNDERİMDEN HEMEN ÖNCE tekrar okur.
// Onay ile gönderim arasında izin kapanabilir (AUD-02). Kanal filtresi
// audience.ChannelFilter'daki sabit SQL'dir.
func (s *Store) OptedIn(ctx context.Context, customerID int64, channel string) (bool, error) {
	col, err := audience.ChannelFilter(audience.Channel(channel))
	if err != nil {
		return false, err
	}
	var ok bool
	err = s.db.QueryRow(ctx, `SELECT `+col+` FROM customers c WHERE c.id = $1`, customerID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // müşteri silinmiş: gönderme
	}
	return ok, err
}

// Outcome bir denemenin DB'ye yazılacak sonucu.
type Outcome struct {
	Status            string // sent | retry_wait | failed | skipped
	ProviderMessageID string
	Delay             time.Duration // retry_wait için
	LastError         string
	HTTPStatus        int // 0 = cevap yok
	AttemptOutcome    string
	Duration          time.Duration
}

// ErrLeaseLost sonucu yazmaya çalışan worker'ın artık satırın sahibi
// olmadığını bildirir (lease doldu, başka worker claim etti).
var ErrLeaseLost = errors.New("lease lost: delivery was reclaimed by another worker")

// Record denemeyi delivery_attempts'e yazar ve teslimat durumunu günceller,
// tek transaction'da.
//
// FENCING: UPDATE "WHERE id = $1 AND status = 'in_flight' AND attempts = $n".
// Lease'i dolmuş yavaş bir worker geç dönerse, satır o arada başka bir claim
// ile attempts = n+1 olmuştur; bu UPDATE 0 satır etkiler ve ErrLeaseLost
// döner. Eski sahibin sonucu yeni sahibinkini EZEMEZ. (Gerçek gönderim
// yine tektir: iki deneme de aynı Idempotency-Key ile gitti.)
//
// Deneme satırı durum güncellemesiyle birlikte yazılır (dashboard'daki
// "son olaylar" ve retry kanıtı). Lease kaybedildiyse ikisi de geri alınır:
// satırın tarihçesini yalnızca güncel sahip yazar.
func (s *Store) Record(ctx context.Context, j Job, o Outcome) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var httpStatus *int
	if o.HTTPStatus != 0 {
		httpStatus = &o.HTTPStatus
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO delivery_attempts (delivery_id, attempt_no, http_status, outcome, duration_ms)
		VALUES ($1, $2, $3, $4, $5)`, j.ID, j.Attempts, httpStatus, o.AttemptOutcome, o.Duration.Milliseconds()); err != nil {
		return fmt.Errorf("insert attempt: %w", err)
	}

	var tag interface{ RowsAffected() int64 }
	switch o.Status {
	case "sent":
		tag, err = tx.Exec(ctx, `
			UPDATE deliveries SET status = 'sent', provider_message_id = $3, sent_at = now(),
			       lease_until = NULL, last_error = NULL, updated_at = now()
			WHERE id = $1 AND status = 'in_flight' AND attempts = $2`, j.ID, j.Attempts, o.ProviderMessageID)
	case "retry_wait":
		tag, err = tx.Exec(ctx, `
			UPDATE deliveries SET status = 'retry_wait', next_attempt_at = now() + make_interval(secs => $3),
			       lease_until = NULL, last_error = $4, updated_at = now()
			WHERE id = $1 AND status = 'in_flight' AND attempts = $2`, j.ID, j.Attempts, o.Delay.Seconds(), o.LastError)
	case "failed", "skipped":
		tag, err = tx.Exec(ctx, `
			UPDATE deliveries SET status = $3, lease_until = NULL, last_error = $4, updated_at = now()
			WHERE id = $1 AND status = 'in_flight' AND attempts = $2`, j.ID, j.Attempts, o.Status, o.LastError)
	default:
		return fmt.Errorf("record: unknown status %q", o.Status)
	}
	if err != nil {
		return fmt.Errorf("update delivery: %w", err)
	}
	if tag.RowsAffected() != 1 {
		// Deneme satırını da geri al: bu worker'ın gözlemi artık geçersiz
		// sahiplikte yapıldı; kayıt tutarlılığı için tek gerçek sahip yazar.
		return ErrLeaseLost
	}
	return tx.Commit(ctx)
}

// FinalizeCampaigns terminal olmayan teslimatı kalmamış "sending"
// kampanyaları kapatır: en az bir failed varsa
// completed_with_errors, yoksa completed. Tek ifade, idempotent: birden
// çok worker ya da tekrar çağrı güvenli.
func (s *Store) FinalizeCampaigns(ctx context.Context) ([]int64, error) {
	rows, err := s.db.Query(ctx, `
		UPDATE campaigns c
		SET status = CASE WHEN EXISTS (SELECT 1 FROM deliveries d WHERE d.campaign_id = c.id AND d.status = 'failed')
		                  THEN 'completed_with_errors' ELSE 'completed' END,
		    completed_at = now(), version = version + 1, updated_at = now()
		WHERE c.status = 'sending'
		  AND NOT EXISTS (SELECT 1 FROM deliveries d
		                  WHERE d.campaign_id = c.id AND d.status IN ('queued', 'in_flight', 'retry_wait'))
		RETURNING c.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// QueueDepth kanal/durum bazında satır sayıları (gauge için).
func (s *Store) QueueDepth(ctx context.Context) (map[[2]string]int, error) {
	rows, err := s.db.Query(ctx, `SELECT channel, status, count(*) FROM deliveries GROUP BY 1, 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[[2]string]int{}
	for rows.Next() {
		var ch, st string
		var n int
		if err := rows.Scan(&ch, &st, &n); err != nil {
			return nil, err
		}
		out[[2]string{ch, st}] = n
	}
	return out, rows.Err()
}
