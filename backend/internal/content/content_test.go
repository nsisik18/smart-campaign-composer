package content

import (
	"slices"
	"strings"
	"testing"
)

var bounds = Bounds{LongestFirstName: "Abdurrahmangazi", LongestCity: "İstanbul"} // 15 ve 8 rune

func codes(is []Issue) []string {
	var out []string
	for _, i := range is {
		out = append(out, i.Path+":"+i.Code)
	}
	return out
}

func valid() Content {
	return Content{
		SMSText:      "Merhaba {{first_name}}, sepetindekiler seni bekliyor! %30 indirimle bugün tamamla.",
		EmailSubject: "{{first_name}}, sepetindeki ürünlerde %30 indirim",
		EmailBody:    "Merhaba {{first_name}},\n\nSepetine eklediğin ürünler hâlâ {{city}} için stokta.",
	}
}

func TestValidContent(t *testing.T) {
	c, ch, issues := Validate(valid(), bounds)
	if issues != nil {
		t.Fatal(issues)
	}
	if c.SMSText == "" || !ch.SMS.OK || ch.SMS.RenderedMax != ch.SMS.TemplateLength-len("{{first_name}}")+15 {
		t.Fatalf("%+v", ch.SMS)
	}
}

// AI-06: limit render edilmiş halde ve rune bazında uygulanır.
func TestLengthAfterRender(t *testing.T) {
	// Şablon tam 160 rune: kendisi sığıyor, en uzun adla render edilince 161 olur.
	tpl := "{{first_name}} " + strings.Repeat("ş", 145) // 14 + 1 + 145 = 160 (boşluk: ek kuralına takılmasın)
	c := valid()
	c.SMSText = tpl
	_, ch, issues := Validate(c, bounds)
	if ch.SMS.TemplateLength != 160 || ch.SMS.RenderedMax != 161 {
		t.Fatalf("%+v", ch.SMS)
	}
	if !slices.Contains(codes(issues), "sms_text:too_long") || !strings.Contains(issues[0].Message, "Abdurrahmangazi") {
		t.Fatalf("%v", issues)
	}

	// Tam 160 rune ama 320 byte: geçerli olmalı (byte sayılmaz).
	c.SMSText = strings.Repeat("ş", 160)
	if _, _, issues := Validate(c, bounds); issues != nil {
		t.Fatalf("160 runes must pass: %v", issues)
	}
	c.SMSText = strings.Repeat("ş", 161)
	if _, _, issues := Validate(c, bounds); !slices.Contains(codes(issues), "sms_text:too_long") {
		t.Fatal("161 runes must fail")
	}

	// Konu: 75 rune şablon, render sonrası 80'i aşar.
	c = valid()
	c.EmailSubject = "{{first_name}} " + strings.Repeat("a", 60) // 75 rune -> 76
	if _, _, issues := Validate(c, bounds); issues != nil {
		t.Fatalf("76 should pass: %v", issues)
	}
	c.EmailSubject = "{{first_name}} {{first_name}} " + strings.Repeat("a", 50) // 80 -> 82
	_, ch, issues = Validate(c, bounds)
	if !slices.Contains(codes(issues), "email_subject:too_long") || ch.Subject.OK {
		t.Fatalf("%v %+v", issues, ch.Subject)
	}
}

// AI-07: bilinmeyen veya bozuk placeholder reddedilir.
func TestPlaceholders(t *testing.T) {
	cases := map[string]string{
		"Merhaba {{last_name}}":          "unknown_placeholder",
		"Merhaba {{ first_name }}":       "unknown_placeholder",
		"Merhaba {{First_Name}}":         "unknown_placeholder",
		"Merhaba {{first_name}":          "broken_placeholder",
		"Merhaba first_name}}":           "broken_placeholder",
		"Merhaba {{first_name}} {{vip}}": "unknown_placeholder",
		"Kod: {{":                        "broken_placeholder",
	}
	for tpl, code := range cases {
		c := valid()
		c.SMSText = tpl
		if _, _, issues := Validate(c, bounds); !slices.Contains(codes(issues), "sms_text:"+code) {
			t.Errorf("%q: want %s, got %v", tpl, code, issues)
		}
	}
	c := valid()
	c.SMSText = "{{first_name}} {{city}} {{first_name}}"
	if _, _, issues := Validate(c, bounds); issues != nil {
		t.Fatalf("repeated allowed placeholders must pass: %v", issues)
	}
}

func TestOtherRules(t *testing.T) {
	c := valid()
	c.EmailSubject = "satır1\nsatır2"
	c.EmailBody = "<html><b>indirim</b></html>"
	c.SMSText = "   "
	_, _, issues := Validate(c, bounds)
	for _, want := range []string{"email_subject:newline", "email_body:html", "sms_text:required"} {
		if !slices.Contains(codes(issues), want) {
			t.Errorf("missing %s in %v", want, codes(issues))
		}
	}
}

func TestNormalizeTrimsButNeverTruncates(t *testing.T) {
	c := valid()
	c.SMSText = "  " + c.SMSText + "\n"
	out, _, issues := Validate(c, bounds)
	if issues != nil || out.SMSText != valid().SMSText {
		t.Fatalf("%q %v", out.SMSText, issues)
	}
}

func TestParseAndValidate(t *testing.T) {
	for _, raw := range []string{
		`{"sms_text":"a","email_subject":"b"`,
		`{"sms_text":"a","email_subject":"b","email_body":"c","html":"x"}`,
		`{"sms_text":"a","email_subject":"b","email_body":"c"} trailing`,
	} {
		if _, _, issues := ParseAndValidate([]byte(raw), bounds); !slices.Contains(codes(issues), "$:invalid_json") {
			t.Errorf("%s: %v", raw, issues)
		}
	}
}

func TestRender(t *testing.T) {
	got := Render("Merhaba {{first_name}}, {{city}} {{first_name}}", Vars{FirstName: "Ayşe", City: "İzmir"})
	if got != "Merhaba Ayşe, İzmir Ayşe" {
		t.Fatal(got)
	}
}

// Gerçek Claude testinde bulundu: "{{city}}'a" Bursa için "Bursa'a" olur.
// Ünlü uyumu değere bağlı, bu yüzden placeholder'a yapışık ek her zaman hata.
func TestPlaceholderSuffixRejected(t *testing.T) {
	bad := []string{
		"{{city}}'da indirim başladı",
		"{{city}}’dan sevgiler", // tipografik kesme işareti
		"{{first_name}}'in sepeti",
		"Selam {{first_name}}cim", // kesme işaretsiz ek
	}
	for _, s := range bad {
		c := valid()
		c.SMSText = s
		if _, _, issues := Validate(c, bounds); !slices.Contains(codes(issues), "sms_text:placeholder_suffix") {
			t.Errorf("%q must be rejected, got %v", s, issues)
		}
	}
	ok := []string{
		"Merhaba {{first_name}}, {{city}} için indirim!",
		"{{city}} şehrindeki fırsatlar: {{first_name}}.",
		"Merhaba {{first_name}}'",
	}
	for _, s := range ok {
		c := valid()
		c.SMSText = s
		if _, _, issues := Validate(c, bounds); issues != nil {
			t.Errorf("%q must pass, got %v", s, issues)
		}
	}
}
