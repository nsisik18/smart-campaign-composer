package content

import (
	"slices"
	"testing"
)

// Model %30 yerine %20 yazarsa, link veya kupon uydurursa yakalanmalı.
func TestCheckFidelity(t *testing.T) {
	brief := "Sepete ekleyip almayan İstanbullulara %30 indirim hatırlat."
	ok := Content{SMSText: "Merhaba {{first_name}}, %30 indirim seni bekliyor!", EmailSubject: "Sepetin seni bekliyor", EmailBody: "Merhaba,\n\n%30 indirim."}
	if is := CheckFidelity(brief, ok); is != nil {
		t.Fatalf("faithful copy flagged: %v", is)
	}

	cases := map[string]struct {
		c    Content
		want string
	}{
		"wrong percent":   {Content{SMSText: "%20 indirim!", EmailSubject: "x", EmailBody: "%30 indirim"}, "sms_text:offer_mismatch"},
		"offer dropped":   {Content{SMSText: "Sepetin seni bekliyor", EmailSubject: "x", EmailBody: "y"}, "content:offer_missing"},
		"invented link":   {Content{SMSText: "%30 indirim: www.ornek.com", EmailSubject: "x", EmailBody: "y"}, "sms_text:invented_link"},
		"invented domain": {Content{SMSText: "%30 indirim", EmailSubject: "x", EmailBody: "magaza.com.tr adresinde"}, "email_body:invented_link"},
		"invented code":   {Content{SMSText: "%30 indirim, kod: KIS30", EmailSubject: "x", EmailBody: "y"}, "sms_text:invented_code"},
	}
	for name, tc := range cases {
		if got := codes(CheckFidelity(brief, tc.c)); !slices.Contains(got, tc.want) {
			t.Errorf("%s: want %s, got %v", name, tc.want, got)
		}
	}

	// Brief'te yüzde yoksa metinde de olmamalı; "yüzde 25" ve "25%" yazımları da yüzdedir.
	if got := codes(CheckFidelity("Ankaralılara bayram kutlaması", Content{SMSText: "%10 hediye!", EmailSubject: "x", EmailBody: "y"})); !slices.Contains(got, "sms_text:offer_mismatch") {
		t.Errorf("invented percent without one in brief: %v", got)
	}
	if is := CheckFidelity("yüzde 25 indirim duyur", Content{SMSText: "%25 indirim", EmailSubject: "25% indirim", EmailBody: "y"}); is != nil {
		t.Errorf("percent spellings must match: %v", is)
	}
	// Brief'teki link cümle sonunda noktalama ile de kullanılabilir.
	for _, s := range []string{"%15 indirim: www.ornek.com.", "%15 indirim www.ornek.com, kaçırma", "%15 indirim (www.ornek.com)"} {
		if is := CheckFidelity("%15 indirim, www.ornek.com", Content{SMSText: s, EmailSubject: "x", EmailBody: "y"}); is != nil {
			t.Errorf("%q: trailing punctuation is not part of the link: %v", s, is)
		}
	}
	// Brief'te verilen link ve kod kullanılabilir.
	if is := CheckFidelity("%15 indirim, kod YAZ15, www.ornek.com", Content{SMSText: "%15 indirim, kod YAZ15", EmailSubject: "x", EmailBody: "www.ornek.com"}); is != nil {
		t.Errorf("link and code from the brief must pass: %v", is)
	}
}
