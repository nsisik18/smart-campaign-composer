package audience

import (
	"fmt"
	"strings"
	"time"
)

// Compile doğrulanmış planı parametreli WHERE ifadesine çevirir.
//
// Sözleşme:
//   - SQL metni YALNIZCA bu dosyadaki sabit parçalardan oluşur. Plan'dan
//     gelen her değer args'a gider; string birleştirmeyle SQL'e girmez.
//   - Tablo alias'ı "c" (customers). Kanal izni burada YOK: izin filtresi
//     segmentten bağımsızdır ve ChannelFilter ile ayrıca eklenir.
//   - now parametredir; test sabit saatle çalışır, gerçek çağrı clock'tan.
//   - Bilinmeyen alan/op'ta hata döner. Validate'ten geçmiş bir planda bu
//     olamaz; yine de "default: geçir" yerine hata, savunma derinliği için.
func Compile(p Plan, now time.Time) (string, []any, error) {
	if len(p.Rules) == 0 {
		return "", nil, fmt.Errorf("compile: empty plan")
	}
	b := &builder{}
	nowArg := ""
	nowRef := func() string {
		if nowArg == "" {
			nowArg = b.arg(now)
		}
		return nowArg + "::timestamptz"
	}

	parts := make([]string, 0, len(p.Rules))
	for i, r := range p.Rules {
		var sql string
		switch r.Field {
		case FieldCity:
			switch r.Op {
			case OpEq:
				sql = "c.city_key = " + b.arg(mustString(r.Value))
			case OpIn:
				sql = "c.city_key = ANY(" + b.arg(mustStrings(r.Value)) + "::text[])"
			}
		case FieldCartAddedWithinDays:
			if r.Op == OpLte {
				sql = fmt.Sprintf("c.last_cart_at >= %s - make_interval(days => %s::int)", nowRef(), b.arg(mustInt(r.Value)))
			}
		case FieldPurchasedSinceLastCart:
			if r.Op == OpEq {
				if mustBool(r.Value) {
					sql = "(c.last_cart_at IS NOT NULL AND c.last_purchase_at >= c.last_cart_at)"
				} else {
					sql = "(c.last_cart_at IS NOT NULL AND (c.last_purchase_at IS NULL OR c.last_purchase_at < c.last_cart_at))"
				}
			}
		case FieldPurchasedWithinDays:
			days := b.arg(mustInt(r.Value))
			switch r.Op {
			case OpLte: // son N günde almış
				sql = fmt.Sprintf("c.last_purchase_at >= %s - make_interval(days => %s::int)", nowRef(), days)
			case OpGt: // son N günde ALMAMIŞ (hiç almamış dahil)
				sql = fmt.Sprintf("(c.last_purchase_at IS NULL OR c.last_purchase_at < %s - make_interval(days => %s::int))", nowRef(), days)
			}
		case FieldTotalOrders:
			n := b.arg(mustInt(r.Value))
			switch r.Op {
			case OpGte:
				sql = "c.total_orders >= " + n
			case OpLte:
				sql = "c.total_orders <= " + n
			case OpEq:
				sql = "c.total_orders = " + n
			}
		}
		if sql == "" {
			return "", nil, fmt.Errorf("compile: rules[%d] unsupported %s/%s", i, r.Field, r.Op)
		}
		parts = append(parts, sql)
	}
	return strings.Join(parts, " AND "), b.args, nil
}

// Channel gönderim kanalı.
type Channel string

const (
	ChannelSMS   Channel = "sms"
	ChannelEmail Channel = "email"
)

// ChannelFilter kanal izni için sabit SQL parçası. Segmentten bağımsız,
// her zaman backend tarafından eklenir; Claude'un etkisi yoktur.
func ChannelFilter(ch Channel) (string, error) {
	switch ch {
	case ChannelSMS:
		return "c.sms_opt_in", nil
	case ChannelEmail:
		return "c.email_opt_in", nil
	}
	return "", fmt.Errorf("unknown channel %q", ch)
}

type builder struct{ args []any }

func (b *builder) arg(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

// must* yardımcıları Validate'in tip garantisine dayanır; ihlal bir
// programlama hatasıdır, bu yüzden panic.
func mustString(v any) string    { return v.(string) }
func mustStrings(v any) []string { return v.([]string) }
func mustInt(v any) int          { return v.(int) }
func mustBool(v any) bool        { return v.(bool) }
