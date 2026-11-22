package campaign

import (
	"context"
	"time"
)

// ChannelStats bir kanalın canlı gönderim durumu.
type ChannelStats struct {
	Total     int `json:"total"`
	Queued    int `json:"queued"`
	InFlight  int `json:"in_flight"`
	RetryWait int `json:"retry_wait"`
	Sent      int `json:"sent"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
	// SentPerSec son 2 s'de sent olan teslimat hızı; RequestsPerSec aynı
	// pencerede provider'a yapılan istek hızı (retry'lar dahil). İkincisi
	// hız sınırının (≤100/s) panoda görünen karşılığı.
	SentPerSec     float64 `json:"sent_per_sec"`
	RequestsPerSec float64 `json:"requests_per_sec"`
}

// AttemptEvent panodaki "son olaylar" satırı. PII yok: alıcı adresi veya
// adı değil, yalnızca teslimat/müşteri ID'si ve teknik sonuç.
type AttemptEvent struct {
	DeliveryID int64     `json:"delivery_id"`
	Channel    string    `json:"channel"`
	AttemptNo  int       `json:"attempt_no"`
	HTTPStatus *int      `json:"http_status"`
	Outcome    string    `json:"outcome"`
	DurationMs int       `json:"duration_ms"`
	At         time.Time `json:"at"`
}

type MonitorStats struct {
	CampaignID  int64                   `json:"campaign_id"`
	Status      Status                  `json:"status"`
	Channels    map[string]ChannelStats `json:"channels"`
	ErrorCodes  map[string]int          `json:"error_codes"` // başarısız deneme kırılımı: "429", "503", "422", "timeout"
	Recent      []AttemptEvent          `json:"recent"`
	ApprovedAt  *time.Time              `json:"approved_at"`
	CompletedAt *time.Time              `json:"completed_at"`
	At          time.Time               `json:"at"`
}

// Terminal kampanya artık değişmeyecek mi.
func (s MonitorStats) Terminal() bool {
	return s.Status == StatusCompleted || s.Status == StatusCompletedWithErrors
}

// MonitorStats panonun tek sorgu turu. Her 500 ms'de bağlı her istemci için
// çalışır; sorgular campaign_id index'leri üzerinde ve ucuzdur. Çok sayıda
// izleyici için tek bir yayıncı (fan-out) gerekir (DECISIONS).
func (r *Repository) MonitorStats(ctx context.Context, id int64) (MonitorStats, error) {
	out := MonitorStats{CampaignID: id, Channels: map[string]ChannelStats{}, ErrorCodes: map[string]int{}, Recent: []AttemptEvent{}}
	err := r.db.QueryRow(ctx, `SELECT status, approved_at, completed_at, now() FROM campaigns WHERE id = $1`, id).
		Scan(&out.Status, &out.ApprovedAt, &out.CompletedAt, &out.At)
	if err != nil {
		if isNoRows(err) {
			return out, ErrNotFound
		}
		return out, err
	}

	rows, err := r.db.Query(ctx, `
		SELECT channel,
		       count(*),
		       count(*) FILTER (WHERE status = 'queued'),
		       count(*) FILTER (WHERE status = 'in_flight'),
		       count(*) FILTER (WHERE status = 'retry_wait'),
		       count(*) FILTER (WHERE status = 'sent'),
		       count(*) FILTER (WHERE status = 'failed'),
		       count(*) FILTER (WHERE status = 'skipped'),
		       count(*) FILTER (WHERE sent_at > now() - interval '2 seconds') / 2.0
		FROM deliveries WHERE campaign_id = $1 GROUP BY channel`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var ch string
		var c ChannelStats
		if err := rows.Scan(&ch, &c.Total, &c.Queued, &c.InFlight, &c.RetryWait, &c.Sent, &c.Failed, &c.Skipped, &c.SentPerSec); err != nil {
			rows.Close()
			return out, err
		}
		out.Channels[ch] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	// İstek hızı ve retry kırılımı delivery_attempts'ten.
	rows, err = r.db.Query(ctx, `
		SELECT d.channel,
		       count(*) FILTER (WHERE a.occurred_at > now() - interval '2 seconds') / 2.0
		FROM delivery_attempts a JOIN deliveries d ON d.id = a.delivery_id
		WHERE d.campaign_id = $1 AND a.occurred_at > now() - interval '2 seconds'
		GROUP BY d.channel`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var ch string
		var rps float64
		if err := rows.Scan(&ch, &rps); err != nil {
			rows.Close()
			return out, err
		}
		c := out.Channels[ch]
		c.RequestsPerSec = rps
		out.Channels[ch] = c
	}
	rows.Close()

	rows, err = r.db.Query(ctx, `
		SELECT CASE WHEN a.outcome = 'timeout' THEN 'timeout' ELSE a.http_status::text END, count(*)
		FROM delivery_attempts a JOIN deliveries d ON d.id = a.delivery_id
		WHERE d.campaign_id = $1 AND a.outcome IN ('retry', 'timeout', 'failed')
		GROUP BY 1`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var k *string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return out, err
		}
		key := "other"
		if k != nil {
			key = *k
		}
		out.ErrorCodes[key] = n
	}
	rows.Close()

	rows, err = r.db.Query(ctx, `
		SELECT a.delivery_id, d.channel, a.attempt_no, a.http_status, a.outcome, a.duration_ms, a.occurred_at
		FROM delivery_attempts a JOIN deliveries d ON d.id = a.delivery_id
		WHERE d.campaign_id = $1
		ORDER BY a.occurred_at DESC, a.id DESC LIMIT 20`, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e AttemptEvent
		if err := rows.Scan(&e.DeliveryID, &e.Channel, &e.AttemptNo, &e.HTTPStatus, &e.Outcome, &e.DurationMs, &e.At); err != nil {
			return out, err
		}
		out.Recent = append(out.Recent, e)
	}
	return out, rows.Err()
}
