package content

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	pctRe  = regexp.MustCompile(`(?i)%\s?(\d{1,3})|(\d{1,3})\s?%|yüzde\s?(\d{1,3})`)
	linkRe = regexp.MustCompile(`(?i)(https?://|www\.)\S+|\b[a-z0-9-]+\.(com|net|org|io|app)(\.tr)?\b`)
	codeRe = regexp.MustCompile(`\b[A-Z]{2,}[0-9]{1,4}\b`) // kupon kodu: KIS30, INDIRIM20
)

// CheckFidelity Claude çıktısının brief'e sadık olduğunu denetler. Yalnızca
// üretimde çalışır; insan düzenlemesinde çalışmaz, çünkü pazarlamacı teklifi
// bilerek değiştirebilir.
//
// Brief'te olmayan yüzde, link veya kupon kodu uydurmadır. Brief'teki yüzde
// metinde hiç geçmiyorsa teklif kaybolmuştur.
func CheckFidelity(brief string, c Content) []Issue {
	want := percents(brief)
	seen := map[string]bool{}
	var issues []Issue
	add := func(path, code, f string, a ...any) {
		issues = append(issues, Issue{Path: path, Code: code, Message: fmt.Sprintf(f, a...)})
	}
	for _, f := range []struct{ path, text string }{
		{"sms_text", c.SMSText}, {"email_subject", c.EmailSubject}, {"email_body", c.EmailBody},
	} {
		for _, p := range sortedKeys(percents(f.text)) {
			seen[p] = true
			if !want[p] {
				add(f.path, "offer_mismatch", "metinde %%%s geçiyor ama brief'te yok", p)
			}
		}
		for _, m := range linkRe.FindAllString(f.text, -1) {
			// Cümle sonundaki nokta, virgül veya kapanan parantez linkin parçası değil.
			m = strings.TrimRight(m, ".,;:!?)]}'\"’”")
			if !strings.Contains(strings.ToLower(brief), strings.ToLower(m)) {
				add(f.path, "invented_link", "brief'te olmayan link: %s", m)
			}
		}
		for _, m := range codeRe.FindAllString(f.text, -1) {
			if !strings.Contains(brief, m) {
				add(f.path, "invented_code", "brief'te olmayan kod: %s", m)
			}
		}
	}
	for _, p := range sortedKeys(want) {
		if !seen[p] {
			add("content", "offer_missing", "brief'teki %%%s teklifi metinde hiç geçmiyor", p)
		}
	}
	return issues
}

func percents(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range pctRe.FindAllStringSubmatch(s, -1) {
		for _, g := range m[1:] {
			if g != "" {
				out[g] = true
			}
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
