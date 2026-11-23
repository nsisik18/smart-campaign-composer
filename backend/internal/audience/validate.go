package audience

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

// rawPlan Claude çıktısının ham şeklidir. Value json.RawMessage'dır çünkü
// tipi alana göre değişir; yorumlama Validate'te yapılır.
type rawPlan struct {
	Summary             string    `json:"summary"`
	Rules               []rawRule `json:"rules"`
	UnsupportedCriteria []string  `json:"unsupported_criteria"`
}

type rawRule struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

// fieldSpec alan başına izinli operatörler ve değer kuralı. Yeni alan
// eklemek = buraya bir satır + compiler'a bir case.
type fieldSpec struct {
	ops   []Op
	parse func(op Op, raw json.RawMessage) (any, string) // (değer, hata mesajı)
}

var specs = map[Field]fieldSpec{
	FieldCity:                   {ops: []Op{OpEq, OpIn}, parse: parseCity},
	FieldCartAddedWithinDays:    {ops: []Op{OpLte}, parse: intRange(1, 365)},
	FieldPurchasedSinceLastCart: {ops: []Op{OpEq}, parse: parseBool},
	FieldPurchasedWithinDays:    {ops: []Op{OpLte, OpGt}, parse: intRange(1, 365)},
	FieldTotalOrders:            {ops: []Op{OpGte, OpLte, OpEq}, parse: intRange(0, 1000)},
}

// CodeNoSupportedCriteria: kural yok, tüm kriterler desteklenmeyen olarak beyan edildi.
const CodeNoSupportedCriteria = "no_supported_criteria"

// UnsupportedOnly çıktı geçerli JSON ise ve TEK sorun "hiçbir kriter
// desteklenmiyor" ise, beyan edilen kriterleri döner. Servis bu durumda
// onarım denemez (veri yoksa ikinci deneme de kural üretemez).
func UnsupportedOnly(raw []byte, issues []Issue) ([]string, bool) {
	if len(issues) != 1 || issues[0].Code != CodeNoSupportedCriteria {
		return nil, false
	}
	var rp rawPlan
	if err := json.Unmarshal(raw, &rp); err != nil {
		return nil, false
	}
	var out []string
	for _, c := range rp.UnsupportedCriteria {
		if c = strings.TrimSpace(c); c != "" && utf8.RuneCountInString(c) <= MaxCriterion {
			out = append(out, c)
		}
	}
	return out, len(out) > 0
}

// FieldNames prompt ve hata mesajları için deterministik sıra.
var FieldNames = []Field{FieldCity, FieldCartAddedWithinDays, FieldPurchasedSinceLastCart, FieldPurchasedWithinDays, FieldTotalOrders}

// ParseAndValidate ham LLM metnini katı JSON olarak çözer ve doğrular.
// Hata varsa Plan sıfır değerdir; hiçbir zaman "kısmen geçerli" plan dönmez.
func ParseAndValidate(raw []byte) (Plan, []Issue) {
	var rp rawPlan
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rp); err != nil {
		return Plan{}, []Issue{{Path: "$", Code: "invalid_json", Message: "çıktı geçerli JSON değil veya bilinmeyen alan içeriyor: " + err.Error()}}
	}
	if dec.More() {
		return Plan{}, []Issue{{Path: "$", Code: "invalid_json", Message: "JSON objesinden sonra fazladan içerik var"}}
	}
	return validate(rp)
}

func validate(rp rawPlan) (Plan, []Issue) {
	var issues []Issue
	add := func(path, code, f string, a ...any) {
		issues = append(issues, Issue{Path: path, Code: code, Message: fmt.Sprintf(f, a...)})
	}

	p := Plan{Summary: strings.TrimSpace(rp.Summary), UnsupportedCriteria: []string{}}
	if p.Summary == "" {
		add("summary", "required", "özet boş olamaz")
	} else if utf8.RuneCountInString(p.Summary) > MaxSummary {
		add("summary", "too_long", "özet en fazla %d karakter olmalı", MaxSummary)
	}

	if len(rp.UnsupportedCriteria) > MaxUnsupported {
		add("unsupported_criteria", "too_many", "en fazla %d desteklenmeyen kriter listelenebilir", MaxUnsupported)
	}
	for i, c := range rp.UnsupportedCriteria {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if utf8.RuneCountInString(c) > MaxCriterion {
			add(fmt.Sprintf("unsupported_criteria[%d]", i), "too_long", "en fazla %d karakter", MaxCriterion)
			continue
		}
		p.UnsupportedCriteria = append(p.UnsupportedCriteria, c)
	}

	switch {
	case len(rp.Rules) == 0 && len(p.UnsupportedCriteria) > 0:
		// Kural yok ama model kriterleri desteklenmeyen olarak beyan etmiş:
		// brief'in istediği kitle bizim veride ifade edilemiyor (örn.
		// "İstanbulkart kullananlar"). Bu bir model hatası değil, kullanıcıya
		// açıklanması gereken bir sonuç; servis onarım denemez.
		add("rules", CodeNoSupportedCriteria, "brief'teki kriterler mevcut alanlarla ifade edilemiyor: %s", strings.Join(p.UnsupportedCriteria, "; "))
	case len(rp.Rules) == 0:
		// Boş kural listesi = tüm müşteri tabanı. v1'de bilinçli olarak
		// desteklenmez; "kuralları yok say, herkese gönder" türü bir brief
		// bu yoldan geçemez (AI-03).
		add("rules", "empty", "en az bir kural gerekli; tüm müşterileri hedefleyen segment desteklenmiyor")
	case len(rp.Rules) > MaxRules:
		add("rules", "too_many", "en fazla %d kural olabilir, %d geldi", MaxRules, len(rp.Rules))
	}

	for i, rr := range rp.Rules {
		if i >= MaxRules {
			break
		}
		path := fmt.Sprintf("rules[%d]", i)
		f := Field(strings.ToLower(strings.TrimSpace(rr.Field)))
		spec, ok := specs[f]
		if !ok {
			add(path+".field", "unknown_field", "bilinmeyen alan %q; izinli alanlar: %s", rr.Field, joinFields())
			continue
		}
		op := Op(strings.ToLower(strings.TrimSpace(rr.Op)))
		if !slices.Contains(spec.ops, op) {
			add(path+".op", "bad_operator", "%s alanı için %q operatörü geçersiz; izinli: %s", f, rr.Op, joinOps(spec.ops))
			continue
		}
		v, msg := spec.parse(op, rr.Value)
		if msg != "" {
			add(path+".value", "bad_value", "%s %s: %s", f, op, msg)
			continue
		}
		p.Rules = append(p.Rules, Rule{Field: f, Op: op, Value: v})
	}

	issues = append(issues, checkConflicts(p.Rules)...)
	if len(issues) > 0 {
		return Plan{}, issues
	}
	return p, nil
}

// checkConflicts aynı alanın tekrarını denetler. Tek meşru tekrar, bir
// aralık oluşturan alt+üst sınır çiftidir (örn. total_orders gte 2 + lte 5,
// purchased_within_days gt 30 + lte 90 = "30-90 gün önce almış").
// Boş küme üreten aralıklar ve diğer tüm tekrarlar reddedilir.
func checkConflicts(rules []Rule) []Issue {
	byField := map[Field][]Rule{}
	for _, r := range rules {
		byField[r.Field] = append(byField[r.Field], r)
	}
	var issues []Issue
	for _, f := range FieldNames {
		rs := byField[f]
		if len(rs) < 2 {
			continue
		}
		bad := func(msg string) {
			issues = append(issues, Issue{Path: "rules", Code: "conflicting_rules", Message: fmt.Sprintf("%s: %s", f, msg)})
		}
		if len(rs) > 2 {
			bad("aynı alan en fazla iki kez (alt ve üst sınır) kullanılabilir")
			continue
		}
		a, b := rs[0], rs[1]
		switch f {
		case FieldTotalOrders:
			lo, hi, ok := pair(a, b, OpGte, OpLte)
			if !ok {
				bad("tekrar ancak gte + lte aralığı olarak kullanılabilir")
			} else if lo > hi {
				bad(fmt.Sprintf("boş aralık: gte %d > lte %d", lo, hi))
			}
		case FieldPurchasedWithinDays:
			// gt N: son N günde almamış; lte M: son M günde almış. Kesişim
			// N < M ise "N-M gün önce almış" aralığıdır.
			n, m, ok := pair(a, b, OpGt, OpLte)
			if !ok {
				bad("tekrar ancak gt + lte aralığı olarak kullanılabilir")
			} else if n >= m {
				bad(fmt.Sprintf("boş aralık: gt %d >= lte %d", n, m))
			}
		default:
			bad("bu alan yalnızca bir kez kullanılabilir")
		}
	}
	return issues
}

// pair iki kuralın op'larının tam olarak (loOp, hiOp) olup olmadığını
// kontrol eder ve değerlerini sırayla döner.
func pair(a, b Rule, loOp, hiOp Op) (int, int, bool) {
	if a.Op == hiOp && b.Op == loOp {
		a, b = b, a
	}
	if a.Op != loOp || b.Op != hiOp {
		return 0, 0, false
	}
	return a.Value.(int), b.Value.(int), true
}

func intRange(lo, hi int) func(Op, json.RawMessage) (any, string) {
	return func(_ Op, raw json.RawMessage) (any, string) {
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, "tam sayı bekleniyordu"
		}
		if f != math.Trunc(f) {
			return nil, fmt.Sprintf("tam sayı bekleniyordu, %v geldi", f)
		}
		n := int(f)
		if n < lo || n > hi {
			return nil, fmt.Sprintf("değer %d..%d aralığında olmalı, %d geldi", lo, hi, n)
		}
		return n, ""
	}
}

func parseBool(_ Op, raw json.RawMessage) (any, string) {
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, "true/false bekleniyordu"
	}
	return b, ""
}

func parseCity(op Op, raw json.RawMessage) (any, string) {
	allowed := "izinli şehirler: " + strings.Join(Cities, ", ")
	if op == OpEq {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, "eq için tek şehir adı (string) bekleniyordu"
		}
		key, ok := NormalizeCity(s)
		if !ok {
			return nil, fmt.Sprintf("bilinmeyen şehir %q; %s", s, allowed)
		}
		return key, ""
	}
	var ss []string
	if err := json.Unmarshal(raw, &ss); err != nil {
		return nil, "in için şehir listesi (string dizisi) bekleniyordu"
	}
	if len(ss) == 0 || len(ss) > len(Cities) {
		return nil, fmt.Sprintf("liste 1..%d şehir içermeli", len(Cities))
	}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		key, ok := NormalizeCity(s)
		if !ok {
			return nil, fmt.Sprintf("bilinmeyen şehir %q; %s", s, allowed)
		}
		if !slices.Contains(out, key) {
			out = append(out, key)
		}
	}
	slices.Sort(out)
	return out, ""
}

func joinFields() string {
	s := make([]string, len(FieldNames))
	for i, f := range FieldNames {
		s[i] = string(f)
	}
	return strings.Join(s, ", ")
}

func joinOps(ops []Op) string {
	s := make([]string, len(ops))
	for i, o := range ops {
		s[i] = string(o)
	}
	return strings.Join(s, ", ")
}
