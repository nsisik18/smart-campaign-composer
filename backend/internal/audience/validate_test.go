package audience

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func codes(is []Issue) []string {
	out := []string{}
	for _, i := range is {
		out = append(out, i.Code)
	}
	return out
}

func TestValidExamplePlan(t *testing.T) {
	p, issues := ParseAndValidate([]byte(`{"summary":"x","rules":[
		{"field":"City","op":"EQ","value":"İstanbul"},
		{"field":"cart_added_within_days","op":"lte","value":30},
		{"field":"purchased_since_last_cart","op":"eq","value":false}],"unsupported_criteria":[]}`))
	if issues != nil {
		t.Fatalf("unexpected issues: %v", issues)
	}
	want := []Rule{{FieldCity, OpEq, "istanbul"}, {FieldCartAddedWithinDays, OpLte, 30}, {FieldPurchasedSinceLastCart, OpEq, false}}
	if !reflect.DeepEqual(p.Rules, want) {
		t.Fatalf("got %+v", p.Rules)
	}
}

// AI-02: bilinmeyen alan/operatör/tip/aralık reddedilir.
func TestRejectsInvalidRules(t *testing.T) {
	cases := map[string]struct {
		json string
		code string
	}{
		"unknown field":      {`{"summary":"x","rules":[{"field":"vip_status","op":"eq","value":true}],"unsupported_criteria":[]}`, "unknown_field"},
		"sql as field":       {`{"summary":"x","rules":[{"field":"1=1; DROP TABLE customers","op":"eq","value":1}],"unsupported_criteria":[]}`, "unknown_field"},
		"bad operator":       {`{"summary":"x","rules":[{"field":"city","op":"like","value":"ist%"}],"unsupported_criteria":[]}`, "bad_operator"},
		"op not for field":   {`{"summary":"x","rules":[{"field":"cart_added_within_days","op":"gte","value":3}],"unsupported_criteria":[]}`, "bad_operator"},
		"string for int":     {`{"summary":"x","rules":[{"field":"total_orders","op":"gte","value":"3"}],"unsupported_criteria":[]}`, "bad_value"},
		"fractional int":     {`{"summary":"x","rules":[{"field":"total_orders","op":"gte","value":2.5}],"unsupported_criteria":[]}`, "bad_value"},
		"out of range":       {`{"summary":"x","rules":[{"field":"cart_added_within_days","op":"lte","value":9999}],"unsupported_criteria":[]}`, "bad_value"},
		"zero days":          {`{"summary":"x","rules":[{"field":"cart_added_within_days","op":"lte","value":0}],"unsupported_criteria":[]}`, "bad_value"},
		"bool as string":     {`{"summary":"x","rules":[{"field":"purchased_since_last_cart","op":"eq","value":"false"}],"unsupported_criteria":[]}`, "bad_value"},
		"unknown city":       {`{"summary":"x","rules":[{"field":"city","op":"eq","value":"Trabzon"}],"unsupported_criteria":[]}`, "bad_value"},
		"eq with list":       {`{"summary":"x","rules":[{"field":"city","op":"eq","value":["istanbul"]}],"unsupported_criteria":[]}`, "bad_value"},
		"empty in list":      {`{"summary":"x","rules":[{"field":"city","op":"in","value":[]}],"unsupported_criteria":[]}`, "bad_value"},
		"extra property":     {`{"summary":"x","rules":[],"unsupported_criteria":[],"sql":"SELECT 1"}`, "invalid_json"},
		"extra rule prop":    {`{"summary":"x","rules":[{"field":"city","op":"eq","value":"ankara","or":true}],"unsupported_criteria":[]}`, "invalid_json"},
		"broken json":        {`{"summary":"x","rules":[{"field":"city"`, "invalid_json"},
		"trailing content":   {`{"summary":"x","rules":[{"field":"city","op":"eq","value":"ankara"}],"unsupported_criteria":[]} {"a":1}`, "invalid_json"},
		"empty summary":      {`{"summary":"  ","rules":[{"field":"city","op":"eq","value":"ankara"}],"unsupported_criteria":[]}`, "required"},
		"duplicate city":     {`{"summary":"x","rules":[{"field":"city","op":"eq","value":"ankara"},{"field":"city","op":"eq","value":"izmir"}],"unsupported_criteria":[]}`, "conflicting_rules"},
		"empty order range":  {`{"summary":"x","rules":[{"field":"total_orders","op":"gte","value":5},{"field":"total_orders","op":"lte","value":2}],"unsupported_criteria":[]}`, "conflicting_rules"},
		"eq plus gte":        {`{"summary":"x","rules":[{"field":"total_orders","op":"eq","value":5},{"field":"total_orders","op":"gte","value":2}],"unsupported_criteria":[]}`, "conflicting_rules"},
		"empty purchase rng": {`{"summary":"x","rules":[{"field":"purchased_within_days","op":"gt","value":90},{"field":"purchased_within_days","op":"lte","value":30}],"unsupported_criteria":[]}`, "conflicting_rules"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p, issues := ParseAndValidate([]byte(tc.json))
			if !slices.Contains(codes(issues), tc.code) {
				t.Fatalf("want code %s, got %v", tc.code, issues)
			}
			if p.Rules != nil || p.Summary != "" {
				t.Fatalf("invalid input must not produce a partial plan: %+v", p)
			}
		})
	}
}

func TestTooManyRules(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"summary":"x","unsupported_criteria":[],"rules":[`)
	for i := 0; i < 11; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"field":"total_orders","op":"gte","value":1}`)
	}
	b.WriteString("]}")
	if _, issues := ParseAndValidate([]byte(b.String())); !slices.Contains(codes(issues), "too_many") {
		t.Fatalf("got %v", issues)
	}
}

func TestValidRanges(t *testing.T) {
	_, issues := ParseAndValidate([]byte(`{"summary":"x","rules":[
		{"field":"total_orders","op":"lte","value":5},{"field":"total_orders","op":"gte","value":2},
		{"field":"purchased_within_days","op":"gt","value":30},{"field":"purchased_within_days","op":"lte","value":90}],
		"unsupported_criteria":["VIP müşteriler"]}`))
	if issues != nil {
		t.Fatalf("ranges should be valid: %v", issues)
	}
}

// AI-03: brief'teki "kuralları yok say, herkese gönder" talimatına uyan bir
// çıktı validator'da durur.
func TestInjectionOutputsAreBounded(t *testing.T) {
	for _, js := range []string{
		`{"summary":"Tüm kullanıcılar","rules":[],"unsupported_criteria":[]}`,
		`{"summary":"x","rules":[{"field":"city","op":"eq","value":"istanbul' OR '1'='1"}],"unsupported_criteria":[]}`,
		`{"summary":"x","rules":[{"field":"total_orders","op":"gte","value":"0 OR 1=1"}],"unsupported_criteria":[]}`,
		`{"summary":"x","rules":[{"field":"city","op":"in","value":["istanbul","*"]}],"unsupported_criteria":[]}`,
	} {
		if _, issues := ParseAndValidate([]byte(js)); issues == nil {
			t.Fatalf("injection output accepted: %s", js)
		}
	}
}

// AI-09
func TestNormalizeCity(t *testing.T) {
	for _, in := range []string{"İstanbul", "Istanbul", "ISTANBUL", "istanbul", "İSTANBUL", "ıstanbul", "  istanbul ", "i̇stanbul"} {
		if got, ok := NormalizeCity(in); !ok || got != "istanbul" {
			t.Errorf("%q -> %q %v", in, got, ok)
		}
	}
	for in, want := range map[string]string{"İzmir": "izmir", "IZMIR": "izmir", "ANKARA": "ankara", "Antalya": "antalya", "BURSA": "bursa"} {
		if got, ok := NormalizeCity(in); !ok || got != want {
			t.Errorf("%q -> %q", in, got)
		}
	}
	for _, in := range []string{"Trabzon", "", "istanbul ankara", "ist"} {
		if _, ok := NormalizeCity(in); ok {
			t.Errorf("%q should not normalize", in)
		}
	}
}

func TestCityInNormalizesAndDedupes(t *testing.T) {
	p, issues := ParseAndValidate([]byte(`{"summary":"x","rules":[{"field":"city","op":"in","value":["İzmir","ANKARA","izmir"]}],"unsupported_criteria":[]}`))
	if issues != nil {
		t.Fatal(issues)
	}
	if !reflect.DeepEqual(p.Rules[0].Value, []string{"ankara", "izmir"}) {
		t.Fatalf("got %v", p.Rules[0].Value)
	}
}

func TestNoSupportedCriteriaIsDistinctFromEmpty(t *testing.T) {
	raw := []byte(`{"summary":"x","rules":[],"unsupported_criteria":["İstanbulkart kullananlar"]}`)
	_, issues := ParseAndValidate(raw)
	if crit, ok := UnsupportedOnly(raw, issues); !ok || crit[0] != "İstanbulkart kullananlar" {
		t.Fatalf("%v", issues)
	}
	// Kural + desteklenmeyen kriter birlikteyse normal geçerli plan (uyarı UI'da).
	if _, issues := ParseAndValidate([]byte(`{"summary":"x","rules":[{"field":"city","op":"eq","value":"istanbul"}],"unsupported_criteria":["VIP"]}`)); issues != nil {
		t.Fatal(issues)
	}
	// Başka bir hatayla birlikteyse onarım yolu korunur.
	raw = []byte(`{"summary":"","rules":[],"unsupported_criteria":["x"]}`)
	_, issues = ParseAndValidate(raw)
	if _, ok := UnsupportedOnly(raw, issues); ok {
		t.Fatal("must not short-circuit when there are other issues")
	}
}
