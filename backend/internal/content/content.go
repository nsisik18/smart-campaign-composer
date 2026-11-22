// Package content kampanya metinlerinin (SMS, email konusu, email gövdesi)
// kurallarını uygular: placeholder allowlist'i, render ve rune bazlı limitler.
//
// Kurallar Go'da zorunludur; frontend yalnızca anlık geri bildirim verir.
// Aynı Validate hem Claude çıktısına hem insan düzenlemesine (Gün 4) uygulanır.
package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	SMSLimit     = 160
	SubjectLimit = 80
	BodyLimit    = 5000
)

// Content Claude'un ürettiği veya insanın düzenlediği şablonlardır.
type Content struct {
	SMSText      string `json:"sms_text"`
	EmailSubject string `json:"email_subject"`
	EmailBody    string `json:"email_body"`
}

// Issue alan bazlı doğrulama hatası. Message hem UI'da gösterilir hem
// onarım çağrısında Claude'a gönderilir.
type Issue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (i Issue) String() string { return i.Path + ": " + i.Message }

// Bounds render kontrolü için kitledeki en uzun değerlerdir.
//
// Not (BLUEPRINT §4.4 düzeltmesi): en uzun ad ve en uzun şehir farklı
// müşterilere ait olabilir; bu yüzden bu kontrol her alıcıyı tek tek render
// etmeye "eşdeğer" değil, onun bir ÜST SINIRIDIR. Uzunluk her placeholder
// değerinde monoton arttığından, bu sınırı geçen şablon hiçbir alıcıda
// limiti aşmaz. Bedeli: nadiren, gerçekte sığacak bir şablon reddedilebilir.
// Güvenli taraf bu. Onay anında her alıcı zaten ayrı render edilir.
type Bounds struct {
	LongestFirstName string
	LongestCity      string // görünen ad, örn. "İstanbul"
}

// Vars bir alıcının placeholder değerleri.
type Vars struct {
	FirstName string `json:"first_name"`
	City      string `json:"city"`
}

// Placeholders izinli placeholder'lar. Template engine yok: düz string replace.
var Placeholders = []string{"{{first_name}}", "{{city}}"}

func Render(tpl string, v Vars) string {
	return strings.NewReplacer("{{first_name}}", v.FirstName, "{{city}}", v.City).Replace(tpl)
}

// RuneLen Unicode code point sayısı; byte değil. Frontend'deki
// Array.from(s).length ile aynı sonucu verir.
func RuneLen(s string) int { return utf8.RuneCountInString(s) }

// FieldCheck bir alanın uzunluk özeti; UI sayaçları için.
type FieldCheck struct {
	TemplateLength int  `json:"template_length"`
	RenderedMax    int  `json:"rendered_max"`
	Limit          int  `json:"limit"`
	OK             bool `json:"ok"`
}

type Checks struct {
	SMS     FieldCheck `json:"sms_text"`
	Subject FieldCheck `json:"email_subject"`
	Body    FieldCheck `json:"email_body"`
	// Preview en uzun değerlerle render edilmiş hali gösterir.
	PreviewVars Vars `json:"preview_vars"`
}

// Normalize dış boşlukları kırpar. İçerik kısaltılmaz; uzun metin sessizce
// kırpılmaz, hata olur (BLUEPRINT §4.5).
func Normalize(c Content) Content {
	return Content{
		SMSText:      strings.TrimSpace(c.SMSText),
		EmailSubject: strings.TrimSpace(c.EmailSubject),
		EmailBody:    strings.TrimSpace(c.EmailBody),
	}
}

// ParseAndValidate ham LLM metnini katı JSON olarak çözer ve doğrular.
func ParseAndValidate(raw []byte, b Bounds) (Content, Checks, []Issue) {
	var c Content
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Content{}, Checks{}, []Issue{{Path: "$", Code: "invalid_json", Message: "çıktı geçerli JSON değil veya bilinmeyen alan içeriyor: " + err.Error()}}
	}
	if dec.More() {
		return Content{}, Checks{}, []Issue{{Path: "$", Code: "invalid_json", Message: "JSON objesinden sonra fazladan içerik var"}}
	}
	return Validate(c, b)
}

// Validate normalize eder ve tüm kuralları uygular. Hata varsa da Checks
// döner (UI sayaçları için), ama Content yalnızca hatasızsa anlamlıdır.
func Validate(c Content, b Bounds) (Content, Checks, []Issue) {
	c = Normalize(c)
	pv := Vars{FirstName: b.LongestFirstName, City: b.LongestCity}
	var issues []Issue
	add := func(path, code, f string, a ...any) {
		issues = append(issues, Issue{Path: path, Code: code, Message: fmt.Sprintf(f, a...)})
	}

	check := func(path, tpl string, limit int, label string) FieldCheck {
		fc := FieldCheck{TemplateLength: RuneLen(tpl), RenderedMax: RuneLen(Render(tpl, pv)), Limit: limit}
		if tpl == "" {
			add(path, "required", "%s boş olamaz", label)
			return fc
		}
		for _, is := range checkPlaceholders(tpl) {
			add(path, is.code, "%s", is.msg)
		}
		if fc.RenderedMax > limit {
			if fc.RenderedMax != fc.TemplateLength {
				add(path, "too_long", "%s kişiselleştirme sonrası %d karakter (en uzun ad %q ile), en fazla %d", label, fc.RenderedMax, pv.FirstName, limit)
			} else {
				add(path, "too_long", "%s %d karakter, en fazla %d", label, fc.RenderedMax, limit)
			}
		}
		fc.OK = fc.RenderedMax <= limit
		return fc
	}

	ch := Checks{PreviewVars: pv}
	ch.SMS = check("sms_text", c.SMSText, SMSLimit, "SMS")
	ch.Subject = check("email_subject", c.EmailSubject, SubjectLimit, "Email konusu")
	if strings.ContainsAny(c.EmailSubject, "\r\n") {
		add("email_subject", "newline", "email konusu tek satır olmalı")
		ch.Subject.OK = false
	}
	ch.Body = check("email_body", c.EmailBody, BodyLimit, "Email gövdesi")
	if strings.Contains(strings.ToLower(c.EmailBody), "<html") || strings.Contains(c.EmailBody, "</") {
		add("email_body", "html", "email gövdesi düz metin olmalı, HTML içeremez")
		ch.Body.OK = false
	}

	// Kod bazlı OK alanları issue'larla tutarlı olsun.
	for _, is := range issues {
		switch is.Path {
		case "sms_text":
			ch.SMS.OK = false
		case "email_subject":
			ch.Subject.OK = false
		case "email_body":
			ch.Body.OK = false
		}
	}
	if issues != nil {
		return Content{}, ch, issues
	}
	return c, ch, nil
}

type phIssue struct{ code, msg string }

// checkPlaceholders izinli placeholder'ları çıkarır; kalan her "{{" veya
// "}}" bilinmeyen ya da bozuk placeholder'dır. "{{ first_name }}" gibi
// boşluklu yazımlar da reddedilir: render düz replace olduğu için müşteriye
// ham "{{ first_name }}" gitmesin.
func checkPlaceholders(tpl string) []phIssue {
	rest := tpl
	for _, p := range Placeholders {
		rest = strings.ReplaceAll(rest, p, "")
	}
	var out []phIssue
	seen := map[string]bool{}
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			break
		}
		j := strings.Index(rest[i:], "}}")
		if j < 0 {
			out = append(out, phIssue{"broken_placeholder", "kapanmamış placeholder: \"" + truncate(rest[i:], 20) + "\""})
			rest = rest[i+2:]
			continue
		}
		name := rest[i : i+j+2]
		if !seen[name] {
			seen[name] = true
			out = append(out, phIssue{"unknown_placeholder", fmt.Sprintf("bilinmeyen placeholder %s; yalnızca %s kullanılabilir", name, strings.Join(Placeholders, ", "))})
		}
		rest = rest[i+j+2:]
	}
	if strings.Contains(rest, "}}") {
		out = append(out, phIssue{"broken_placeholder", "açılmamış placeholder kapanışı \"}}\""})
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
