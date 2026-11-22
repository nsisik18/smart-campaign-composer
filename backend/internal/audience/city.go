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
// Türkçe casefold tuzağı: strings.ToLower("İ") "i̇" (i + U+0307 birleşik
// nokta) üretir, ToLowerSpecial(TurkishCase, "I") ise "ı" üretir. İkisi de
// "istanbul" ile eşleşmez. Bu yüzden önce dört i varyantını (İ, I, ı, i)
// düz "i"ye indirir, birleşik noktayı atar, kalan Türkçe harfleri ASCII'ye
// katlarız. Kanonik key'ler zaten ASCII olduğundan karşılaştırma tek yönlüdür.
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
