package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
	"github.com/insider-case/smart-campaign-composer/internal/content"
)

type Repository struct{ db dbtx }

// dbtx pool'un kullandığımız yüzü (Approve için Begin dahil).
type dbtx interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

const cols = `id, brief, status, version, segment, sms_text, email_subject, email_body, created_at, updated_at`

func (r *Repository) Create(ctx context.Context, brief string) (Campaign, error) {
	return scan(r.db.QueryRow(ctx,
		`INSERT INTO campaigns (brief, status) VALUES ($1, 'draft') RETURNING `+cols, brief))
}

func (r *Repository) Get(ctx context.Context, id int64) (Campaign, error) {
	return scan(r.db.QueryRow(ctx, `SELECT `+cols+` FROM campaigns WHERE id = $1`, id))
}

// SaveSegment optimistic lock ile segmenti yazar: yalnızca versiyon hâlâ
// expectedVersion ve durum analiz edilebilir ise. LLM çağrısı sırasında satır
// kilitli tutulmaz; bunun yerine yazma anında versiyon karşılaştırılır.
func (r *Repository) SaveSegment(ctx context.Context, id int64, expectedVersion int, p audience.Plan) (Campaign, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return Campaign{}, err
	}
	c, err := scan(r.db.QueryRow(ctx, `
		UPDATE campaigns SET segment = $3, status = 'analyzed', version = version + 1, updated_at = now()
		WHERE id = $1 AND version = $2 AND status IN ('draft', 'analyzed')
		RETURNING `+cols, id, expectedVersion, b))
	if errors.Is(err, ErrNotFound) {
		// Satır var ama koşul tutmadı mı, yoksa satır hiç yok mu?
		return Campaign{}, r.conflictOrMissing(ctx, id, "campaign changed during analysis")
	}
	return c, err
}

func scan(row pgx.Row) (Campaign, error) {
	var c Campaign
	var seg []byte
	var sms, subj, body *string
	err := row.Scan(&c.ID, &c.Brief, &c.Status, &c.Version, &seg, &sms, &subj, &body, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Campaign{}, ErrNotFound
	}
	if err != nil {
		return Campaign{}, err
	}
	if seg != nil {
		// DB'deki segment bizim normalize ettiğimiz plandır; yine de aynı
		// validator'dan geçirerek tipli hale getiririz (şema kayması da yakalanır).
		p, issues := audience.ParseAndValidate(seg)
		if issues != nil {
			return Campaign{}, fmt.Errorf("stored segment for campaign %d is invalid: %v", c.ID, issues)
		}
		c.Segment = &p
	}
	if sms != nil && subj != nil && body != nil {
		c.Content = &content.Content{SMSText: *sms, EmailSubject: *subj, EmailBody: *body}
	}
	return c, nil
}

// BeginGeneration yeni bir içerik üretimi için versiyonu artırır ve yeni
// versiyonu döner. Üretim sonucu yalnızca bu versiyonla kaydedilebilir
// (SaveContent): arada başka bir üretim/analiz başladıysa eski stream'in
// sonucu yeni durumun üzerine yazamaz.
func (r *Repository) BeginGeneration(ctx context.Context, id int64) (Campaign, error) {
	c, err := scan(r.db.QueryRow(ctx, `
		UPDATE campaigns SET version = version + 1, updated_at = now()
		WHERE id = $1 AND status IN ('analyzed', 'generated') AND segment IS NOT NULL
		RETURNING `+cols, id))
	if errors.Is(err, ErrNotFound) {
		return Campaign{}, r.conflictOrMissing(ctx, id, "content can only be generated after audience analysis and before approval")
	}
	return c, err
}

// SaveContent üretilmiş (ve doğrulanmış) içeriği yazar; versiyon hâlâ
// genVersion değilse 409.
func (r *Repository) SaveContent(ctx context.Context, id int64, genVersion int, ct content.Content) (Campaign, error) {
	c, err := scan(r.db.QueryRow(ctx, `
		UPDATE campaigns SET sms_text = $3, email_subject = $4, email_body = $5,
		       status = 'generated', version = version + 1, updated_at = now()
		WHERE id = $1 AND version = $2 AND status IN ('analyzed', 'generated')
		RETURNING `+cols, id, genVersion, ct.SMSText, ct.EmailSubject, ct.EmailBody))
	if errors.Is(err, ErrNotFound) {
		return Campaign{}, r.conflictOrMissing(ctx, id, "a newer generation or change superseded this one")
	}
	return c, err
}

func (r *Repository) conflictOrMissing(ctx context.Context, id int64, reason string) error {
	cur, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	return &ConflictError{Status: cur.Status, Version: cur.Version, Reason: reason}
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
