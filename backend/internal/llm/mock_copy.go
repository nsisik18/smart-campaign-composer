package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var copyFixtureRe = regexp.MustCompile(`\[\[copy:([a-z_]+)\]\]`)

// copyJSON şemadaki alan sırasını korur (map alfabetik sıralardı); gerçek
// Claude da alanları şema sırasıyla üretir, böylece demo'da SMS önce akar.
func copyJSON(sms, subject, body string) string {
	b, _ := json.Marshal(struct {
		SMS     string `json:"sms_text"`
		Subject string `json:"email_subject"`
		Body    string `json:"email_body"`
	}{sms, subject, body})
	return string(b)
}

var validCopy = copyJSON(
	"Merhaba {{first_name}}, sepetindekiler seni bekliyor! Bugüne özel %30 indirimle alışverişini tamamla.",
	"{{first_name}}, sepetindeki ürünlerde %30 indirim seni bekliyor",
	"Merhaba {{first_name}},\n\nSepetine eklediğin ürünleri unutmadık. Bugüne özel %30 indirimle alışverişini şimdi tamamlayabilirsin.\n\nİndirim sınırlı süre geçerli; ürünler tükenmeden göz atmanı öneririz.\n\nSevgiler,\nEkibimiz",
)

// copyFixtures: ad -> (ilk çıktı, onarım çıktısı, akış sonunda dönecek hata).
// Hatalı fixture'larda metin yine stream edilir: kesilme ve timeout
// gerçekte de kısmi metinden sonra gelir.
var copyFixtures = map[string]struct {
	first, repaired string
	err             error
}{
	"valid":        {first: validCopy},
	"invalid_json": {first: `{"sms_text":"Merhaba {{first_name}}","email_subject":"İndirim" "email_body":"x"}`},
	"overlong_sms": {first: copyJSON(strings.Repeat("Sepetindeki ürünler seni bekliyor! ", 6), "Sepetinde %30 indirim", "Merhaba,\n\nİndirim seni bekliyor.")},
	// Şablon 79 karakter (sığıyor) ama en uzun adla render edilince 80'i aşar.
	"overlong_subject_after_render": {first: copyJSON("Merhaba {{first_name}}, %30 indirim seni bekliyor.",
		"{{first_name}}, sepetindeki ürünlerde bugüne özel %30 indirim fırsatını kaçırma!", "Merhaba {{first_name}},\n\nİndirim seni bekliyor.")},
	"unknown_placeholder": {first: copyJSON("Merhaba {{first_name}} {{last_name}}, kupon kodun {{coupon}}.", "Sepetinde %30 indirim", "Merhaba,\n\nİndirim seni bekliyor.")},
	"invalid_then_repaired": {
		first:    copyJSON(strings.Repeat("Sepetindeki ürünler seni bekliyor! ", 6), "Sepetinde %30 indirim", "Merhaba,\n\nİndirim seni bekliyor."),
		repaired: validCopy,
	},
	"truncated": {first: `{"sms_text":"Merhaba {{first_name}}, sepetindekiler seni bek`, err: ErrTruncated},
	"timeout":   {first: `{"sms_text":"Merhaba {{first_name}}`, err: ErrTimeout},
	"refusal":   {err: ErrRefusal},
}

var percentRe = regexp.MustCompile(`%\s?(\d{1,2})`)

func (m *Mock) StreamCopy(ctx context.Context, req CopyRequest, onText func(string)) (Result, error) {
	m.calls.Add(1)
	if req.Repair != nil {
		m.repair.Add(1)
	}
	res := Result{Model: "mock", PromptVersion: CopyPromptVersion}

	text, ferr := validCopy, error(nil)
	if mm := copyFixtureRe.FindStringSubmatch(req.Brief); mm != nil {
		f, ok := copyFixtures[mm[1]]
		if !ok {
			return res, fmt.Errorf("%w: unknown mock copy fixture %q", ErrUpstream, mm[1])
		}
		text, ferr = f.first, f.err
		if req.Repair != nil && f.repaired != "" {
			text, ferr = f.repaired, nil
		}
	} else if mm := percentRe.FindStringSubmatch(req.Brief); mm != nil {
		text = strings.ReplaceAll(validCopy, "%30", "%"+mm[1])
	}

	// Gerçek stream'i taklit: rune sınırlarında 3-11 byte'lık parçalar.
	var sb strings.Builder
	for i, n := 0, 0; i < len(text); n++ {
		size := 3 + (n*7)%9
		j := min(len(text), i+size)
		for j < len(text) && !utf8.RuneStart(text[j]) {
			j++
		}
		chunk := text[i:j]
		i = j
		if m.ChunkDelay > 0 {
			select {
			case <-ctx.Done():
				res.Text = sb.String()
				return res, ErrTimeout
			case <-time.After(m.ChunkDelay):
			}
		}
		sb.WriteString(chunk)
		onText(chunk)
	}
	res.Text = sb.String()
	return res, ferr
}
