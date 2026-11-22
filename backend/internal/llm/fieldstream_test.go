package llm

import (
	"encoding/json"
	"math/rand/v2"
	"testing"
)

var copyFields = []string{"sms_text", "email_subject", "email_body"}

// Kasıtlı olarak zor bir payload: escape'ler, \u, surrogate çifti (emoji),
// ham çok baytlı UTF-8, farklı alan sırası, bilinmeyen alan, boşluklar.
const tricky = "{ \"email_subject\" : \"{{first_name}}, \\\"%30\\\" indirim \\u00e7ok yak\\u0131nda \\ud83d\\ude00\",\n" +
	"\"unknown\":\"görünmemeli\",\"sms_text\":\"Merhaba {{first_name}}\\nŞimdi al 🛒 \\\\ ışık\\/yol\\t!\"," +
	"\"email_body\":\"Satır1\\r\\nSatır2 — ğüşıöç ÇĞÜŞİÖ\"}"

func decodeWant(t *testing.T, raw string) map[string]string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "unknown")
	return m
}

func feedAll(chunks []string) (map[string]string, *FieldStream) {
	p := NewFieldStream(copyFields...)
	got := map[string]string{}
	for _, c := range chunks {
		for _, d := range p.Feed(c) {
			got[d.Field] += d.Text
		}
	}
	return got, p
}

func check(t *testing.T, got, want map[string]string, p *FieldStream, desc string) {
	t.Helper()
	if p.Err() != nil || !p.Done() {
		t.Fatalf("%s: err=%v done=%v", desc, p.Err(), p.Done())
	}
	for _, f := range copyFields {
		if got[f] != want[f] {
			t.Fatalf("%s: field %s\n got %q\nwant %q", desc, f, got[f], want[f])
		}
	}
	if _, ok := got["unknown"]; ok {
		t.Fatalf("%s: unknown field leaked", desc)
	}
}

// AI-08: her byte pozisyonunda iki parçaya bölmek (escape ortası, \u
// ortası, surrogate çifti arası, çok baytlı UTF-8 ortası dahil).
func TestFieldStreamEverySplit(t *testing.T) {
	want := decodeWant(t, tricky)
	for i := 0; i <= len(tricky); i++ {
		got, p := feedAll([]string{tricky[:i], tricky[i:]})
		check(t, got, want, p, "split at "+itoa(i))
	}
}

func TestFieldStreamByteByByteAndRandom(t *testing.T) {
	want := decodeWant(t, tricky)
	var one []string
	for i := 0; i < len(tricky); i++ {
		one = append(one, tricky[i:i+1])
	}
	got, p := feedAll(one)
	check(t, got, want, p, "byte by byte")

	r := rand.New(rand.NewPCG(1, 2))
	for n := 0; n < 500; n++ {
		var chunks []string
		for i := 0; i < len(tricky); {
			j := min(len(tricky), i+1+r.IntN(9))
			chunks = append(chunks, tricky[i:j])
			i = j
		}
		got, p := feedAll(chunks)
		check(t, got, want, p, "random")
	}
}

// Delta'lar hiçbir zaman yarım UTF-8 karakter içermez.
func TestFieldStreamNeverEmitsBrokenUTF8(t *testing.T) {
	for i := 0; i <= len(tricky); i++ {
		p := NewFieldStream(copyFields...)
		for _, c := range []string{tricky[:i], tricky[i:]} {
			for _, d := range p.Feed(c) {
				if !json.Valid([]byte(`"`+jsonEscape(d.Text)+`"`)) || containsRuneError(d.Text) {
					t.Fatalf("split %d: broken delta %q", i, d.Text)
				}
			}
		}
	}
}

func TestFieldStreamRejectsInvalid(t *testing.T) {
	for _, raw := range []string{
		`[1,2]`,
		`{"sms_text": 42}`,
		`{"sms_text":"a" "b"}`,
		`{"sms_text":"\x"}`,
		`{"sms_text":"\u12G4"}`,
		"{\"sms_text\":\"a\nb\"}", // kaçışsız newline
		`{"sms_text":"a"} {`,
	} {
		p := NewFieldStream(copyFields...)
		p.Feed(raw)
		if p.Err() == nil {
			t.Errorf("%q: expected error", raw)
		}
		p.Feed(`more data`) // hatadan sonra panic yok, sessiz
	}
}

func TestFieldStreamTruncatedIsNotDone(t *testing.T) {
	p := NewFieldStream(copyFields...)
	out := p.Feed(`{"sms_text":"Merhaba Ay`)
	if p.Done() || p.Err() != nil || len(out) != 1 || out[0].Text != "Merhaba Ay" {
		t.Fatalf("%v %v %+v", p.Done(), p.Err(), out)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func jsonEscape(s string) string { b, _ := json.Marshal(s); return string(b[1 : len(b)-1]) }

func containsRuneError(s string) bool {
	for _, r := range s {
		if r == '�' {
			return true
		}
	}
	return false
}

// Eşsiz surrogate'ler encoding/json ile aynı sonucu vermeli (U+FFFD).
func TestFieldStreamLoneSurrogatesMatchEncodingJSON(t *testing.T) {
	for _, v := range []string{`\ud83d\n`, `\ud83dx`, `\ude00`, `\ud83d\ud83d\ude00`, `\ud83d\"`, `a\ud83d`} {
		raw := `{"sms_text":"` + v + `","email_subject":"s","email_body":"b"}`
		want := decodeWant(t, raw)
		for i := 0; i <= len(raw); i++ {
			got, p := feedAll([]string{raw[:i], raw[i:]})
			if p.Err() != nil || got["sms_text"] != want["sms_text"] {
				t.Fatalf("%s split %d: got %q want %q err=%v", v, i, got["sms_text"], want["sms_text"], p.Err())
			}
		}
	}
}
