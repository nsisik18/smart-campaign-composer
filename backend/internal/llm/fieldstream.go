package llm

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// FieldDelta bir alanın yeni çözülmüş metin parçası.
type FieldDelta struct {
	Field string `json:"field"`
	Text  string `json:"text"`
}

// FieldStream düz, yalnızca string alanlı bir JSON objesini parça parça
// okuyup her alanın ÇÖZÜLMÜŞ metnini artımlı olarak verir.
//
// Neden: Claude structured output'u stream ederken text delta'ları ham JSON
// metninin parçalarıdır ("{\"sms_te", "xt\":\"Merhaba\\n", ...). UI'a ham
// JSON değil, alan bazlı düz metin akıtmak istiyoruz. Regex ile yarım JSON
// ayıklamak escape'lerde ve parça sınırlarında kırılır; bu yüzden küçük bir
// durum makinesi.
//
// Kapsam bilerek dar: tek seviye obje, değerler yalnızca string. Bu şemayı
// düz tutmamızın sebebi tam olarak bu parser'ın küçük ve tamamen test
// edilebilir kalması. Başka bir şey görürse Err() döner ve susar.
//
// Parser yalnızca ÖNİZLEME içindir. Otoriter içerik, stream bitince tam
// metnin json.Unmarshal + content.Validate ile doğrulanmış halidir.
type FieldStream struct {
	allowed map[string]bool
	st      state
	key     strings.Builder
	field   string // şu an okunan değerin alanı; izinli değilse ""
	pending []byte // çözülmüş ama henüz gönderilmemiş değer byte'ları
	hex     []byte
	high    rune // eşini bekleyen yüksek surrogate (\uD83D gibi)
	err     error
}

type state int

const (
	sStart state = iota
	sKeyOrEnd
	sKey
	sKeyEsc
	sColon
	sValue
	sStr
	sEsc
	sHex
	sSurBackslash // yüksek surrogate'ten sonra "\" bekleniyor
	sSurU         // ... ve "u"
	sAfterValue
	sNextKey
	sDone
	sErr
)

func NewFieldStream(fields ...string) *FieldStream {
	a := map[string]bool{}
	for _, f := range fields {
		a[f] = true
	}
	return &FieldStream{allowed: a}
}

func (p *FieldStream) Err() error { return p.err }

// Done obje kapanış parantezine kadar okundu mu.
func (p *FieldStream) Done() bool { return p.st == sDone }

// Feed bir sonraki ham parçayı işler ve bu parçada tamamlanan metni döner.
// Parça sınırı herhangi bir byte'ta olabilir: escape ortası, \u dizisi
// ortası, çok baytlı UTF-8 karakter ortası.
func (p *FieldStream) Feed(chunk string) []FieldDelta {
	var out []FieldDelta
	emit := func(final bool) {
		if p.field == "" || len(p.pending) == 0 {
			p.pending = p.pending[:0]
			return
		}
		cut := len(p.pending)
		if !final {
			cut = completeUTF8Prefix(p.pending)
		}
		if cut == 0 {
			return
		}
		text := strings.ToValidUTF8(string(p.pending[:cut]), "�")
		if n := len(out); n > 0 && out[n-1].Field == p.field {
			out[n-1].Text += text
		} else {
			out = append(out, FieldDelta{Field: p.field, Text: text})
		}
		p.pending = append(p.pending[:0], p.pending[cut:]...)
	}

	for i := 0; i < len(chunk) && p.st != sErr; i++ {
		c := chunk[i]
		switch p.st {
		case sStart:
			if isWS(c) {
				continue
			}
			p.expect(c == '{', c, "'{'", sKeyOrEnd)
		case sKeyOrEnd, sNextKey:
			if isWS(c) {
				continue
			}
			if c == '}' && p.st == sKeyOrEnd {
				p.st = sDone
				continue
			}
			p.key.Reset()
			p.expect(c == '"', c, "anahtar", sKey)
		case sKey:
			switch c {
			case '\\':
				p.st = sKeyEsc
			case '"':
				p.st = sColon
			default:
				p.key.WriteByte(c)
			}
		case sKeyEsc:
			p.key.WriteByte(c) // anahtarlar sabit ASCII; escape'i literal almak yeterli
			p.st = sKey
		case sColon:
			if isWS(c) {
				continue
			}
			p.expect(c == ':', c, "':'", sValue)
		case sValue:
			if isWS(c) {
				continue
			}
			if c != '"' {
				p.fail(fmt.Errorf("alan %q için string değer bekleniyordu, %q geldi", p.key.String(), c))
				continue
			}
			p.field = ""
			if p.allowed[p.key.String()] {
				p.field = p.key.String()
			}
			p.st = sStr
		case sStr:
			switch {
			case c == '\\':
				p.st = sEsc
			case c == '"':
				emit(true)
				p.field = ""
				p.st = sAfterValue
			case c < 0x20:
				p.fail(fmt.Errorf("string içinde kaçışsız kontrol karakteri 0x%02x", c))
			default:
				p.pending = append(p.pending, c)
			}
		case sEsc:
			if r, ok := simpleEscape(c); ok {
				p.pending = append(p.pending, r)
				p.st = sStr
			} else if c == 'u' {
				p.hex = p.hex[:0]
				p.st = sHex
			} else {
				p.fail(fmt.Errorf("geçersiz escape \\%c", c))
			}
		case sHex:
			if !isHex(c) {
				p.fail(fmt.Errorf("geçersiz \\u dizisi"))
				continue
			}
			p.hex = append(p.hex, c)
			if len(p.hex) < 4 {
				continue
			}
			r := hexRune(p.hex)
			switch {
			case p.high != 0 && utf16.IsSurrogate(r) && r >= 0xDC00:
				p.pending = utf8.AppendRune(p.pending, utf16.DecodeRune(p.high, r))
				p.high = 0
				p.st = sStr
			case r >= 0xD800 && r < 0xDC00: // yüksek surrogate: eşini bekle
				if p.high != 0 {
					p.pending = utf8.AppendRune(p.pending, utf8.RuneError)
				}
				p.high = r
				p.st = sSurBackslash
			default:
				if p.high != 0 {
					p.pending = utf8.AppendRune(p.pending, utf8.RuneError)
					p.high = 0
				}
				if utf16.IsSurrogate(r) { // eşsiz düşük surrogate
					r = utf8.RuneError
				}
				p.pending = utf8.AppendRune(p.pending, r)
				p.st = sStr
			}
		case sSurBackslash, sSurU:
			want := byte('\\')
			if p.st == sSurU {
				want = 'u'
			}
			if c == want {
				if p.st == sSurBackslash {
					p.st = sSurU
				} else {
					p.hex = p.hex[:0]
					p.st = sHex
				}
				continue
			}
			// Eşsiz yüksek surrogate: encoding/json gibi U+FFFD yaz ve byte'ı
			// yeniden işle. "\" zaten tüketildiyse (sSurU) bu byte başka bir
			// escape'in harfidir (örn. "\ud83d\n"), değilse düz içeriktir.
			p.pending = utf8.AppendRune(p.pending, utf8.RuneError)
			p.high = 0
			if p.st == sSurU {
				p.st = sEsc
			} else {
				p.st = sStr
			}
			i--
		case sAfterValue:
			if isWS(c) {
				continue
			}
			switch c {
			case ',':
				p.st = sNextKey
			case '}':
				p.st = sDone
			default:
				p.fail(fmt.Errorf("',' veya '}' bekleniyordu, %q geldi", c))
			}
		case sDone:
			if !isWS(c) {
				p.fail(fmt.Errorf("obje kapandıktan sonra fazladan içerik"))
			}
		}
	}
	if p.st == sStr || p.st == sEsc || p.st == sHex || p.st == sSurBackslash || p.st == sSurU {
		emit(false)
	}
	return out
}

func (p *FieldStream) expect(ok bool, c byte, what string, next state) {
	if !ok {
		p.fail(fmt.Errorf("%s bekleniyordu, %q geldi", what, c))
		return
	}
	p.st = next
}

func (p *FieldStream) fail(err error) {
	if p.err == nil {
		p.err = err
	}
	p.st = sErr
}

// completeUTF8Prefix sonda yarım kalmış çok baytlı karakteri dışarıda
// bırakan en uzun önekin uzunluğu.
func completeUTF8Prefix(b []byte) int {
	// Son karakterin başlangıcını bul (en fazla 3 byte geri).
	for back := 1; back <= utf8.UTFMax && back <= len(b); back++ {
		start := len(b) - back
		if utf8.RuneStart(b[start]) {
			if utf8.FullRune(b[start:]) {
				return len(b)
			}
			return start
		}
	}
	return len(b)
}

func simpleEscape(c byte) (byte, bool) {
	switch c {
	case '"', '\\', '/':
		return c, true
	case 'n':
		return '\n', true
	case 't':
		return '\t', true
	case 'r':
		return '\r', true
	case 'b':
		return '\b', true
	case 'f':
		return '\f', true
	}
	return 0, false
}

func isWS(c byte) bool  { return c == ' ' || c == '\n' || c == '\r' || c == '\t' }
func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func hexRune(h []byte) rune {
	var r rune
	for _, c := range h {
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c-'a') + 10
		default:
			r |= rune(c-'A') + 10
		}
	}
	return r
}
