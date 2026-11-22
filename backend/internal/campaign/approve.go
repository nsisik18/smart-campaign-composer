package campaign

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
	"github.com/insider-case/smart-campaign-composer/internal/content"
	"github.com/insider-case/smart-campaign-composer/internal/delivery"
)

// InvalidContentError insan düzenlemesi veya onay anı kontrolü kurallara
// uymadı (422). Issues alan bazlıdır.
type InvalidContentError struct {
	Issues []content.Issue
}

func (e *InvalidContentError) Error() string {
	return fmt.Sprintf("content invalid: %d issue(s)", len(e.Issues))
}

// QueuedCounts onayda kuyruğa alınan teslimat sayıları.
type QueuedCounts struct {
	SMS   int `json:"sms"`
	Email int `json:"email"`
}

// UpdateContent insan düzenlemesini yazar: yalnızca generated durumda ve
// versiyon hâlâ expectedVersion ise. Doğrulama serviste, yazmadan önce.
func (r *Repository) UpdateContent(ctx context.Context, id int64, expectedVersion int, ct content.Content) (Campaign, error) {
	c, err := scan(r.db.QueryRow(ctx, `
		UPDATE campaigns SET sms_text = $3, email_subject = $4, email_body = $5,
		       version = version + 1, updated_at = now()
		WHERE id = $1 AND version = $2 AND status = 'generated'
		RETURNING `+cols, id, expectedVersion, ct.SMSText, ct.EmailSubject, ct.EmailBody))
	if errors.Is(err, ErrNotFound) {
		return Campaign{}, r.conflictOrMissing(ctx, id, "campaign changed or is no longer editable")
	}
	return c, err
}

// Approve onay transaction'ıdır (BLUEPRINT §6.1). Tek transaction içinde:
//
//  1. Kampanya satırı FOR UPDATE ile kilitlenir. İki sekmeden eşzamanlı
//     onayda ikinci istek burada bekler; birincisi commit edince durumu
//     "sending" görür ve 409 alır (APP-01).
//  2. Durum "generated" ve versiyon expectedVersion değilse 409. Kullanıcı
//     ekranda gördüğü içeriği onaylar; arada düzenleme/üretim olduysa
//     eski görünümle onay verilemez.
//  3. Her kanal için izinli alıcılar AYNI transaction'da okunur: alıcı
//     listesi ve kampanya durumu tutarlı bir anlık görüntüdür.
//  4. Her alıcı için içerik render edilir ve uzunluk TEK TEK kontrol edilir
//     (üretimdeki üst sınır kontrolünün kesin hali). Tek ihlal tüm onayı
//     geri alır; kısmi kuyruk olmaz.
//  5. Teslimatlar tek INSERT ... SELECT unnest(...) ile yazılır.
//     ON CONFLICT DO NOTHING ikinci savunma hattıdır: uq_delivery ve
//     uq_delivery_key aynı kişiye aynı kanaldan ikinci satırı yapısal olarak
//     engeller (1. adım zaten tekrar onayı durdurur).
//  6. Kampanya "sending" olur, versiyon artar. Onaydan sonra içerik değişmez:
//     UpdateContent/SaveContent "generated" durumu şart koşar (APP-02).
//
// Hata olursa defer'deki Rollback her şeyi geri alır; Commit'ten sonra
// Rollback no-op'tur.
func (r *Repository) Approve(ctx context.Context, id int64, expectedVersion int, now time.Time) (Campaign, QueuedCounts, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Campaign{}, QueuedCounts{}, err
	}
	defer tx.Rollback(ctx)

	// 1-2. Kilit + durum/versiyon kontrolü.
	c, err := scan(tx.QueryRow(ctx, `SELECT `+cols+` FROM campaigns WHERE id = $1 FOR UPDATE`, id))
	if err != nil {
		return Campaign{}, QueuedCounts{}, err
	}
	if c.Status != StatusGenerated || c.Version != expectedVersion {
		return Campaign{}, QueuedCounts{}, &ConflictError{Status: c.Status, Version: c.Version,
			Reason: "campaign can only be approved in generated state at the version you reviewed"}
	}
	if c.Segment == nil || c.Content == nil {
		return Campaign{}, QueuedCounts{}, &ConflictError{Status: c.Status, Version: c.Version, Reason: "segment or content missing"}
	}

	// 3. Alıcılar, aynı transaction'da.
	aud := audience.NewRepository(tx)
	smsTo, err := aud.Recipients(ctx, *c.Segment, now, audience.ChannelSMS)
	if err != nil {
		return Campaign{}, QueuedCounts{}, err
	}
	emailTo, err := aud.Recipients(ctx, *c.Segment, now, audience.ChannelEmail)
	if err != nil {
		return Campaign{}, QueuedCounts{}, err
	}
	if len(smsTo)+len(emailTo) == 0 {
		return Campaign{}, QueuedCounts{}, &ConflictError{Status: c.Status, Version: c.Version, Reason: "audience has no reachable recipients"}
	}

	// Placeholder kuralları ve normalizasyon son kez (bounds burada önemsiz:
	// asıl uzunluk kontrolü aşağıda alıcı bazında).
	ct, _, issues := content.Validate(*c.Content, content.Bounds{})
	if issues != nil {
		return Campaign{}, QueuedCounts{}, &InvalidContentError{Issues: issues}
	}

	// 4. Alıcı bazlı render + uzunluk.
	var rows deliveryRows
	for _, rc := range smsTo {
		body := content.Render(ct.SMSText, varsOf(rc))
		if n := content.RuneLen(body); n > content.SMSLimit {
			return Campaign{}, QueuedCounts{}, &InvalidContentError{Issues: []content.Issue{{Path: "sms_text", Code: "too_long",
				Message: fmt.Sprintf("müşteri #%d için SMS %d karakter, en fazla %d", rc.CustomerID, n, content.SMSLimit)}}}
		}
		rows.add(c.ID, rc, "sms", nil, body)
	}
	for _, rc := range emailTo {
		v := varsOf(rc)
		subj := content.Render(ct.EmailSubject, v)
		if n := content.RuneLen(subj); n > content.SubjectLimit {
			return Campaign{}, QueuedCounts{}, &InvalidContentError{Issues: []content.Issue{{Path: "email_subject", Code: "too_long",
				Message: fmt.Sprintf("müşteri #%d için konu %d karakter, en fazla %d", rc.CustomerID, n, content.SubjectLimit)}}}
		}
		rows.add(c.ID, rc, "email", &subj, content.Render(ct.EmailBody, v))
	}

	// 5. Toplu INSERT; tek round-trip.
	tag, err := tx.Exec(ctx, `
		INSERT INTO deliveries (campaign_id, customer_id, channel, recipient, rendered_subject, rendered_body, idempotency_key)
		SELECT $1, u.customer_id, u.channel, u.recipient, u.subject, u.body, u.key
		FROM unnest($2::bigint[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[])
		     AS u(customer_id, channel, recipient, subject, body, key)
		ON CONFLICT DO NOTHING`,
		c.ID, rows.customer, rows.channel, rows.recipient, rows.subject, rows.body, rows.key)
	if err != nil {
		return Campaign{}, QueuedCounts{}, fmt.Errorf("insert deliveries: %w", err)
	}
	if int(tag.RowsAffected()) != len(rows.customer) {
		// 1. adımdaki kilit ve durum kontrolü varken olmamalı; olursa sessizce
		// eksik kuyrukla devam etmek yerine geri al.
		return Campaign{}, QueuedCounts{}, fmt.Errorf("insert deliveries: %d of %d rows inserted (pre-existing deliveries?)", tag.RowsAffected(), len(rows.customer))
	}

	// 6. Durum geçişi. Kanonik normalize içerik de yazılır (Validate trim etti).
	c, err = scan(tx.QueryRow(ctx, `
		UPDATE campaigns SET status = 'sending', approved_at = now(), version = version + 1, updated_at = now(),
		       sms_text = $2, email_subject = $3, email_body = $4
		WHERE id = $1 RETURNING `+cols, id, ct.SMSText, ct.EmailSubject, ct.EmailBody))
	if err != nil {
		return Campaign{}, QueuedCounts{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Campaign{}, QueuedCounts{}, err
	}
	return c, QueuedCounts{SMS: len(smsTo), Email: len(emailTo)}, nil
}

// DeliveryTotals kampanyanın kanal bazlı teslimat sayıları (onaydan sonra).
func (r *Repository) DeliveryTotals(ctx context.Context, id int64) (QueuedCounts, error) {
	var q QueuedCounts
	err := r.db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE channel = 'sms'), count(*) FILTER (WHERE channel = 'email')
		FROM deliveries WHERE campaign_id = $1`, id).Scan(&q.SMS, &q.Email)
	return q, err
}

func varsOf(r audience.Recipient) content.Vars {
	return content.Vars{FirstName: r.FirstName, City: audience.CityDisplay(r.CityKey)}
}

// deliveryRows unnest için sütun dizileri.
type deliveryRows struct {
	customer                      []int64
	channel, recipient, body, key []string
	subject                       []*string
}

func (d *deliveryRows) add(campaignID int64, r audience.Recipient, ch string, subject *string, body string) {
	d.customer = append(d.customer, r.CustomerID)
	d.channel = append(d.channel, ch)
	d.recipient = append(d.recipient, r.Address)
	d.subject = append(d.subject, subject)
	d.body = append(d.body, body)
	d.key = append(d.key, delivery.IdempotencyKey(campaignID, r.CustomerID, ch))
}
