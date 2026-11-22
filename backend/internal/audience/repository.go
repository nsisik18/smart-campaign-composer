package audience

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Reach UI'daki dört sayı (BLUEPRINT §4.3).
type Reach struct {
	Matched       int `json:"matched"`
	SMSEligible   int `json:"sms_eligible"`
	EmailEligible int `json:"email_eligible"`
	Reachable     int `json:"reachable"` // en az bir kanaldan izinli
}

type Recipient struct {
	CustomerID int64
	FirstName  string
	CityKey    string
	Address    string // telefon veya email; kanal neyse
}

// Querier pgxpool.Pool ve pgx.Tx'in ortak yüzü. Onay transaction'ı alıcıları
// kendi transaction'ı içinde okuyabilsin diye repository ikisiyle de çalışır.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct{ db Querier }

func NewRepository(db Querier) *Repository { return &Repository{db: db} }

// Count tek sorguda dört sayıyı döner. İzin filtreleri FILTER içinde sabit SQL.
func (r *Repository) Count(ctx context.Context, p Plan, now time.Time) (Reach, error) {
	where, args, err := Compile(p, now)
	if err != nil {
		return Reach{}, err
	}
	var out Reach
	err = r.db.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE c.sms_opt_in),
		       count(*) FILTER (WHERE c.email_opt_in),
		       count(*) FILTER (WHERE c.sms_opt_in OR c.email_opt_in)
		FROM customers c WHERE `+where, args...).
		Scan(&out.Matched, &out.SMSEligible, &out.EmailEligible, &out.Reachable)
	return out, err
}

// Recipients bir kanal için izinli alıcıları döner. Onay transaction'ı
// (Gün 4) bunu kullanacak; kanal filtresi segment derlemesinden ayrı eklenir.
func (r *Repository) Recipients(ctx context.Context, p Plan, now time.Time, ch Channel) ([]Recipient, error) {
	where, args, err := Compile(p, now)
	if err != nil {
		return nil, err
	}
	perm, err := ChannelFilter(ch)
	if err != nil {
		return nil, err
	}
	addr := "c.phone"
	if ch == ChannelEmail {
		addr = "c.email"
	}
	rows, err := r.db.Query(ctx, `SELECT c.id, c.first_name, c.city_key, `+addr+`
		FROM customers c WHERE `+perm+` AND (`+where+`) ORDER BY c.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recipient
	for rows.Next() {
		var x Recipient
		if err := rows.Scan(&x.CustomerID, &x.FirstName, &x.CityKey, &x.Address); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// RenderBounds içerik uzunluk kontrolü için kitledeki en uzun adı ve
// kitlede geçen şehirleri döner. char_length Postgres'te karakter (code
// point) sayar; Go'daki rune sayımıyla aynı. Kanal izni bilerek uygulanmaz:
// eşleşen kümenin tamamı, alıcı kümelerinin üst kümesidir (daha güvenli sınır).
func (r *Repository) RenderBounds(ctx context.Context, p Plan, now time.Time) (string, []string, error) {
	where, args, err := Compile(p, now)
	if err != nil {
		return "", nil, err
	}
	var name string
	var cities []string
	err = r.db.QueryRow(ctx, `
		SELECT COALESCE((SELECT c.first_name FROM customers c WHERE `+where+`
		                 ORDER BY char_length(c.first_name) DESC, c.id LIMIT 1), ''),
		       COALESCE((SELECT array_agg(DISTINCT c.city_key) FROM customers c WHERE `+where+`), '{}')`,
		args...).Scan(&name, &cities)
	return name, cities, err
}
