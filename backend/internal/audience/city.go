package audience

import (
	"strings"
	"unicode"
)

// Cities izinli şehir key'leridir; seed ve prompt ile aynı liste.
// Sıra prompt'ta ve UI'da kullanılır.
var Cities = []string{"istanbul", "ankara", "izmir", "bursa", "antalya"}

var cityDisplay = map[string]string{
	"istanbul": "İstanbul", "ankara": "Ankara", "izmir": "İzmir", "bursa": "Bursa", "antalya": "Antalya",
}

// CityDisplay kanonik key'in Türkçe görünen adını döner.
func CityDisplay(key string) string { return cityDisplay[key] }

// NormalizeCity serbest yazılmış şehir adını kanonik key'e çevirir.
//
// Dört i varyantı (İ, I, ı, i) düz "i"ye indirilir: Türkçe kuralla
// (ToLowerSpecial) "I" "ı" olur ve eşleşmez; "İ" ayrışmış yazılmışsa
// (i + U+0307) birleşik nokta atılır. Kalan Türkçe harfler ASCII'ye katlanır,
// çünkü kanonik key'ler ASCII.
func NormalizeCity(s string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch r {
		case 'İ', 'I', 'ı':
			r = 'i'
		case '̇': // birleşik nokta (İ'nin ayrışmış hali)
			continue
		case 'Ş', 'ş':
			r = 's'
		case 'Ç', 'ç':
			r = 'c'
		case 'Ğ', 'ğ':
			r = 'g'
		case 'Ö', 'ö':
			r = 'o'
		case 'Ü', 'ü':
			r = 'u'
		}
		b.WriteRune(unicode.ToLower(r))
	}
	key := b.String()
	for _, c := range Cities {
		if c == key {
			return c, true
		}
	}
	return "", false
}
