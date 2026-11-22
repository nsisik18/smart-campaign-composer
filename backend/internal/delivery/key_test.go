package delivery

import "testing"

func TestIdempotencyKey(t *testing.T) {
	k := IdempotencyKey(1, 42, "sms")
	if len(k) != 64 || k != IdempotencyKey(1, 42, "sms") {
		t.Fatalf("key must be deterministic 64-hex: %s", k)
	}
	// Bilinen değer: format değişirse (örn. ayırıcı) testi bilerek kırar;
	// çalışan bir kampanyanın ortasında anahtar formatı değişmemeli.
	if want := "d6797aec094863927a7ffa0607c3d75b6838689fb531bfda1e34fe86d5e8268e"; k != want { // = sha256("1:42:sms"), shasum ile bağımsız hesaplandı
		t.Fatalf("key format changed: %s", k)
	}
	seen := map[string]bool{}
	for _, k := range []string{
		IdempotencyKey(1, 42, "sms"), IdempotencyKey(1, 42, "email"),
		IdempotencyKey(2, 42, "sms"), IdempotencyKey(1, 43, "sms"),
		IdempotencyKey(14, 2, "sms"), IdempotencyKey(1, 42, "sms "),
	} {
		if seen[k] {
			t.Fatalf("collision: %s", k)
		}
		seen[k] = true
	}
}
