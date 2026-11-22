// Package seed deterministik sahte müşteri verisi üretir.
//
// Determinizm: tüm rastgelelik sabit seed'li PCG'den gelir; aynı (Seed, N)
// her kurulumda aynı müşterileri üretir. Zaman alanları mutlak tarih değil,
// Anchor'a göre ofsettir: seed bugün de bir ay sonra da çalıştırılsa "son 30
// gün" kohortu aynı kalır. Testler Anchor'ı sabit verir.
//
// Garanti kohort: tam olarak CohortSize İstanbullu müşteri, Anchor'dan
// 1..28 gün önce sepete ekleyip sonrasında alışveriş yapmamıştır. Diğer
// İstanbullu müşteriler bu kriteri BİLEREK sağlamaz (sepet >= 32 gün önce,
// sepet yok veya sepetten sonra alışveriş var). Böylece örnek brief'in
// segmenti tam olarak CohortSize kişi döner ve integration testi bu sayıyı
// bilir. Diğer şehirler serbest dağılımlıdır.
package seed

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

const (
	DefaultSeed = 20260101
	DefaultN    = 5000
	CohortSize  = 450
	cohortCity  = "istanbul"
	day         = 24 * time.Hour
)

type Customer struct {
	FirstName      string
	LastName       string
	Email          string
	Phone          string
	CityKey        string
	Language       string
	SMSOptIn       bool
	EmailOptIn     bool
	LastCartAt     *time.Time
	LastPurchaseAt *time.Time
	TotalOrders    int
	// InCohort yalnızca test/istatistik içindir, DB'ye yazılmaz.
	InCohort bool
}

type Options struct {
	Seed   uint64
	N      int
	Anchor time.Time
}

// Şehir dağılımı (BLUEPRINT §5.1). Kümülatif ağırlık.
var cities = []struct {
	key string
	w   float64
}{
	{"istanbul", 0.40}, {"ankara", 0.20}, {"izmir", 0.15}, {"bursa", 0.15}, {"antalya", 0.10},
}

// Uzun isimler bilinçli: render sonrası uzunluk kontrolü gerçek veriyle sınansın.
var firstNames = []string{
	"Ahmet", "Mehmet", "Ayşe", "Fatma", "Zeynep", "Elif", "Emre", "Can", "Deniz", "Ece",
	"Burak", "Selin", "Oğuz", "Gökçe", "İrem", "Çağla", "Şule", "Ümit", "Yusuf", "Merve",
	"Kerem", "Defne", "Arda", "Ilgın", "Barış", "Nazlı", "Tuğba", "Serkan", "Hülya", "Onur",
}
var longFirstNames = []string{"Abdurrahmangazi", "Muhammedmustafa", "Gülizarnurcihan"}
var lastNames = []string{
	"Yılmaz", "Kaya", "Demir", "Şahin", "Çelik", "Yıldız", "Yıldırım", "Öztürk", "Aydın", "Özdemir",
	"Arslan", "Doğan", "Kılıç", "Aslan", "Çetin", "Kara", "Koç", "Kurt", "Özkan", "Şimşek",
	"Karamustafaoğlu",
}

// Generate N müşteriyi üretir. Saf fonksiyon: DB'ye dokunmaz, test edilebilir.
func Generate(o Options) []Customer {
	r := rand.New(rand.NewPCG(o.Seed, o.Seed^0x9e3779b97f4a7c15))
	out := make([]Customer, 0, o.N)
	cohortLeft := CohortSize

	for i := 0; i < o.N; i++ {
		c := Customer{
			CityKey:  pickCity(r),
			Language: "tr",
		}
		if r.Float64() < 0.10 {
			c.Language = "en"
		}
		if r.Float64() < 0.01 {
			c.FirstName = longFirstNames[r.IntN(len(longFirstNames))]
		} else {
			c.FirstName = firstNames[r.IntN(len(firstNames))]
		}
		c.LastName = lastNames[r.IntN(len(lastNames))]
		// i indeksi email ve telefonu benzersiz yapar; UNIQUE constraint çakışmaz.
		c.Email = fmt.Sprintf("%s.%s.%d@example.test", asciiFold(c.FirstName), asciiFold(c.LastName), i+1)
		c.Phone = fmt.Sprintf("+90555%07d", i+1)
		c.SMSOptIn, c.EmailOptIn = pickOptIns(r)

		switch {
		case c.CityKey == cohortCity && cohortLeft > 0 && r.Float64() < 0.30:
			// ~2000 İstanbullunun ~%30'u aday; 450'ye ulaşınca durur.
			cohortLeft--
			c.InCohort = true
			cart := o.Anchor.Add(-jitterDays(r, 1, 28))
			c.LastCartAt = &cart
			c.TotalOrders = r.IntN(6)
			if c.TotalOrders > 0 {
				// Son alışveriş sepetten ÖNCE.
				p := cart.Add(-jitterDays(r, 1, 200))
				c.LastPurchaseAt = &p
			}
		case c.CityKey == cohortCity:
			fillNonMatching(r, o.Anchor, &c)
		default:
			fillRandom(r, o.Anchor, &c)
		}
		out = append(out, c)
	}
	if cohortLeft > 0 {
		// N çok küçükse kohort dolmayabilir; sessizce eksik üretmek yerine
		// patlarız ki "garanti" kelimesi doğru kalsın.
		panic(fmt.Sprintf("seed: cohort underfilled by %d; increase N", cohortLeft))
	}
	return out
}

// fillNonMatching İstanbullu ama kohort dışı müşteri: örnek segmente
// kesinlikle UYMAYAN üç profilden biri.
func fillNonMatching(r *rand.Rand, anchor time.Time, c *Customer) {
	switch r.IntN(3) {
	case 0: // hiç sepet yok
		c.TotalOrders = r.IntN(15)
		if c.TotalOrders > 0 {
			p := anchor.Add(-jitterDays(r, 1, 365))
			c.LastPurchaseAt = &p
		}
	case 1: // eski sepet (>= 32 gün; 30 günlük pencereye zaman kaysa da girmez)
		cart := anchor.Add(-jitterDays(r, 32, 365))
		c.LastCartAt = &cart
		c.TotalOrders = r.IntN(10)
		if c.TotalOrders > 0 {
			p := anchor.Add(-jitterDays(r, 1, 365))
			c.LastPurchaseAt = &p
		}
	default: // yakın sepet ama sonrasında alışveriş var
		cart := anchor.Add(-jitterDays(r, 2, 28))
		c.LastCartAt = &cart
		p := cart.Add(time.Duration(1+r.IntN(24)) * time.Hour)
		c.LastPurchaseAt = &p
		c.TotalOrders = 1 + r.IntN(20)
	}
}

func fillRandom(r *rand.Rand, anchor time.Time, c *Customer) {
	if r.Float64() < 0.75 {
		cart := anchor.Add(-jitterDays(r, 1, 120))
		c.LastCartAt = &cart
	}
	c.TotalOrders = r.IntN(20)
	if c.TotalOrders > 0 {
		p := anchor.Add(-jitterDays(r, 1, 365))
		c.LastPurchaseAt = &p
	}
}

// pickOptIns marjinalleri SMS ~%65, email ~%80, ikisi de kapalı ~%10 yapar:
// none %10, both %55, sms-only %10, email-only %25.
func pickOptIns(r *rand.Rand) (sms, email bool) {
	x := r.Float64()
	switch {
	case x < 0.10:
		return false, false
	case x < 0.65:
		return true, true
	case x < 0.75:
		return true, false
	default:
		return false, true
	}
}

func pickCity(r *rand.Rand) string {
	x := r.Float64()
	acc := 0.0
	for _, c := range cities {
		acc += c.w
		if x < acc {
			return c.key
		}
	}
	return cities[len(cities)-1].key
}

// jitterDays [minD, maxD] gün aralığında, saat/dakika ile dağıtılmış süre.
func jitterDays(r *rand.Rand, minD, maxD int) time.Duration {
	d := minD + r.IntN(maxD-minD+1)
	return time.Duration(d)*day - time.Duration(r.IntN(12*60))*time.Minute
}

var foldReplacer = strings.NewReplacer(
	"ç", "c", "Ç", "c", "ğ", "g", "Ğ", "g", "ı", "i", "İ", "i",
	"ö", "o", "Ö", "o", "ş", "s", "Ş", "s", "ü", "u", "Ü", "u",
)

func asciiFold(s string) string { return strings.ToLower(foldReplacer.Replace(s)) }
