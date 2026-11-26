package seed

import (
	"testing"
	"time"
)

var anchor = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

// matchesExampleSegment örnek brief'in segmentinin Go'daki
// birebir karşılığıdır: city=istanbul AND cart son 30 gün AND sepetten
// sonra alışveriş yok. Integration testi
// aynı sayıyı SQL tarafında doğrular.
func matchesExampleSegment(c Customer, now time.Time) bool {
	if c.CityKey != "istanbul" || c.LastCartAt == nil {
		return false
	}
	if c.LastCartAt.Before(now.Add(-30 * day)) {
		return false
	}
	return c.LastPurchaseAt == nil || c.LastPurchaseAt.Before(*c.LastCartAt)
}

func TestGenerateDeterministic(t *testing.T) {
	a := Generate(Options{Seed: DefaultSeed, N: DefaultN, Anchor: anchor})
	b := Generate(Options{Seed: DefaultSeed, N: DefaultN, Anchor: anchor})
	if len(a) != DefaultN {
		t.Fatalf("len=%d", len(a))
	}
	for i := range a {
		if a[i].Email != b[i].Email || a[i].CityKey != b[i].CityKey || a[i].SMSOptIn != b[i].SMSOptIn ||
			!eqTime(a[i].LastCartAt, b[i].LastCartAt) || !eqTime(a[i].LastPurchaseAt, b[i].LastPurchaseAt) {
			t.Fatalf("row %d differs between runs", i)
		}
	}
}

func TestGuaranteedCohortIsExact(t *testing.T) {
	rows := Generate(Options{Seed: DefaultSeed, N: DefaultN, Anchor: anchor})
	cohort, matched := 0, 0
	for _, c := range rows {
		m := matchesExampleSegment(c, anchor)
		if m {
			matched++
		}
		if c.InCohort {
			cohort++
			if !m {
				t.Fatalf("cohort member does not match segment: %+v", c)
			}
		}
		if m && !c.InCohort {
			t.Fatalf("non-cohort customer leaks into segment: %+v", c)
		}
	}
	if cohort != CohortSize || matched != CohortSize {
		t.Fatalf("cohort=%d matched=%d want %d", cohort, matched, CohortSize)
	}
	// Seed'den 1 gün sonra da kohort sabit kalmalı (demo ertesi gün çalışsın).
	later := 0
	for _, c := range rows {
		if matchesExampleSegment(c, anchor.Add(day)) {
			later++
		}
	}
	if later != CohortSize {
		t.Fatalf("cohort drifted after 1 day: %d", later)
	}
}

func TestDistributions(t *testing.T) {
	rows := Generate(Options{Seed: DefaultSeed, N: DefaultN, Anchor: anchor})
	var sms, email, none, ist, long int
	emails, phones := map[string]bool{}, map[string]bool{}
	for _, c := range rows {
		if c.SMSOptIn {
			sms++
		}
		if c.EmailOptIn {
			email++
		}
		if !c.SMSOptIn && !c.EmailOptIn {
			none++
		}
		if c.CityKey == "istanbul" {
			ist++
		}
		if len([]rune(c.FirstName)) >= 15 {
			long++
		}
		if emails[c.Email] || phones[c.Phone] {
			t.Fatalf("duplicate email/phone: %s %s", c.Email, c.Phone)
		}
		emails[c.Email], phones[c.Phone] = true, true
		if c.LastCartAt != nil && c.LastCartAt.After(anchor) || c.LastPurchaseAt != nil && c.LastPurchaseAt.After(anchor) {
			t.Fatalf("timestamp in the future: %+v", c)
		}
	}
	n := float64(len(rows))
	within := func(name string, got int, want, tol float64) {
		if r := float64(got) / n; r < want-tol || r > want+tol {
			t.Errorf("%s ratio %.3f not within %.2f±%.2f", name, r, want, tol)
		}
	}
	within("sms", sms, 0.65, 0.03)
	within("email", email, 0.80, 0.03)
	within("none", none, 0.10, 0.02)
	within("istanbul", ist, 0.40, 0.03)
	if long == 0 {
		t.Error("expected at least one long first name for render-length checks")
	}
}

func eqTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
