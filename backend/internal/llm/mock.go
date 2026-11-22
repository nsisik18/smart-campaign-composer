package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/insider-case/smart-campaign-composer/internal/audience"
)

// Mock deterministik LLMProvider'dır. Testler ve key'siz demo için.
//
// Seçim sırası:
//  1. Brief'te "[[fixture:AD]]" varsa o fixture (test ve demo için açık seçim).
//  2. Yoksa brief'teki anahtar kelimelerden basit bir plan kurulur
//     (şehir adları, "son N gün", "sepet", "almayan/yapmayan", "VIP").
//
// Mock bilerek "akıllı" değildir: amacı gerçek Claude'u taklit etmek değil,
// doğrulama/onarım yollarını deterministik olarak tetiklemektir.
type Mock struct {
	calls  atomic.Int64
	repair atomic.Int64
	// ChunkDelay içerik stream'inde parçalar arası bekleme (demo için ~25ms;
	// testlerde 0).
	ChunkDelay time.Duration
}

func NewMock() *Mock { return &Mock{} }

// Calls toplam çağrı, Repairs onarım çağrısı sayısı (testler için).
func (m *Mock) Calls() int64   { return m.calls.Load() }
func (m *Mock) Repairs() int64 { return m.repair.Load() }

var fixtureRe = regexp.MustCompile(`\[\[fixture:([a-z_]+)\]\]`)

const validSegment = `{"summary":"Son 30 günde sepete ekleyip sonrasında alışveriş yapmayan İstanbullu kullanıcılar","rules":[{"field":"city","op":"eq","value":"İstanbul"},{"field":"cart_added_within_days","op":"lte","value":30},{"field":"purchased_since_last_cart","op":"eq","value":false}],"unsupported_criteria":[]}`

// segmentFixtures: ad -> (ilk çağrı çıktısı, onarım çağrısı çıktısı, hata).
// Onarım çıktısı boşsa onarımda da ilk çıktı döner (onarım başarısız senaryosu).
var segmentFixtures = map[string]struct {
	first, repaired string
	err             error
}{
	"valid":         {first: validSegment},
	"invalid_json":  {first: `{"summary":"Yarım kalmış","rules":[{"field":"city"`},
	"unknown_field": {first: `{"summary":"VIP İstanbullular","rules":[{"field":"vip_status","op":"eq","value":true},{"field":"city","op":"eq","value":"istanbul"}],"unsupported_criteria":[]}`},
	"bad_operator":  {first: `{"summary":"İstanbul","rules":[{"field":"city","op":"like","value":"ist%"}],"unsupported_criteria":[]}`},
	"bad_value":     {first: `{"summary":"Son 9999 gün","rules":[{"field":"cart_added_within_days","op":"lte","value":9999}],"unsupported_criteria":[]}`},
	// Model brief'teki talimata uymuş gibi davranır: kural yok = herkes.
	"injection_all_users": {first: `{"summary":"Tüm kullanıcılar","rules":[],"unsupported_criteria":[]}`},
	// Değere SQL sıkıştırma denemesi.
	"injection_sql_value": {first: `{"summary":"x","rules":[{"field":"city","op":"eq","value":"istanbul' OR '1'='1"}],"unsupported_criteria":[]}`},
	"invalid_then_repaired": {
		first:    `{"summary":"VIP İstanbullular","rules":[{"field":"vip_status","op":"eq","value":true}],"unsupported_criteria":[]}`,
		repaired: `{"summary":"İstanbullu kullanıcılar","rules":[{"field":"city","op":"eq","value":"istanbul"}],"unsupported_criteria":["VIP müşteriler"]}`,
	},
	"truncated": {err: ErrTruncated},
	"refusal":   {err: ErrRefusal},
	"timeout":   {err: ErrTimeout},
}

func (m *Mock) Segment(ctx context.Context, req SegmentRequest) (Result, error) {
	m.calls.Add(1)
	if req.Repair != nil {
		m.repair.Add(1)
	}
	res := Result{Model: "mock", PromptVersion: SegmentPromptVersion}
	if err := ctx.Err(); err != nil {
		return res, ErrTimeout
	}
	if mm := fixtureRe.FindStringSubmatch(req.Brief); mm != nil {
		f, ok := segmentFixtures[mm[1]]
		if !ok {
			return res, fmt.Errorf("%w: unknown mock fixture %q", ErrUpstream, mm[1])
		}
		if f.err != nil {
			return res, f.err
		}
		res.Text = f.first
		if req.Repair != nil && f.repaired != "" {
			res.Text = f.repaired
		}
		return res, nil
	}
	res.Text = heuristicSegment(req.Brief)
	return res, nil
}

var daysRe = regexp.MustCompile(`(\d{1,3})\s*gün`)

// heuristicSegment anahtar kelimelerden plan üretir. Hiçbir kriter
// bulunamazsa boş kural listesi döner; validator bunu reddeder (doğru davranış).
func heuristicSegment(brief string) string {
	type rule struct {
		Field string `json:"field"`
		Op    string `json:"op"`
		Value any    `json:"value"`
	}
	lower := strings.ToLower(brief)
	var rules []rule
	var unsupported []string
	var summary []string

	var cities []string
	for _, tok := range strings.FieldsFunc(brief, func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' && r != '’' }) {
		// "İstanbul'daki" -> "İstanbul": kesme işaretinden sonraki eki at.
		parts := strings.FieldsFunc(tok, func(r rune) bool { return r == '\'' || r == '’' })
		if len(parts) == 0 {
			continue
		}
		tok = parts[0]
		// Türkçe ek kesme işaretsiz de gelebilir ("İstanbullulara"): şehir
		// adıyla BAŞLAYAN token'ı da kabul et. Gerçek Claude bunu kendisi çözer.
		for _, city := range audience.Cities {
			r := []rune(tok)
			if len(r) < len([]rune(city)) {
				continue
			}
			if key, ok := audience.NormalizeCity(string(r[:len([]rune(city))])); ok && key == city && !contains(cities, key) {
				cities = append(cities, key)
			}
		}
	}
	switch len(cities) {
	case 0:
	case 1:
		rules = append(rules, rule{"city", "eq", cities[0]})
		summary = append(summary, audience.CityDisplay(cities[0])+"'daki")
	default:
		rules = append(rules, rule{"city", "in", cities})
		summary = append(summary, "seçili şehirlerdeki")
	}

	days := 0
	if m := daysRe.FindStringSubmatch(lower); m != nil {
		days, _ = strconv.Atoi(m[1])
	}
	if strings.Contains(lower, "sepet") {
		if days == 0 {
			days = 30
		}
		rules = append(rules, rule{"cart_added_within_days", "lte", days})
		summary = append(summary, fmt.Sprintf("son %d günde sepete ekleyen", days))
		if strings.Contains(lower, "almayan") || strings.Contains(lower, "yapmayan") || strings.Contains(lower, "tamamlamayan") {
			rules = append(rules, rule{"purchased_since_last_cart", "eq", false})
			summary = append(summary, "sonrasında alışveriş yapmayan")
		}
	}
	if strings.Contains(lower, "vip") {
		unsupported = append(unsupported, "VIP müşteriler")
	}
	if rules == nil {
		rules = []rule{}
	}
	if unsupported == nil {
		unsupported = []string{}
	}
	s := strings.Join(summary, ", ") + " kullanıcılar"
	if len(summary) == 0 {
		s = "Brief'ten hedef kitle çıkarılamadı (mock)"
	}
	b, _ := json.Marshal(map[string]any{"summary": s, "rules": rules, "unsupported_criteria": unsupported})
	return string(b)
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
