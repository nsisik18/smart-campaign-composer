// Package delivery gönderim tarafıdır (worker). Gün 4'te yalnızca onay
// transaction'ının da kullandığı idempotency anahtarı burada; claim,
// limiter, retry ve dispatcher Gün 5'te eklenecek.
package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// IdempotencyKey bir teslimatın provider'a giden Idempotency-Key'idir
// (BLUEPRINT §5, §6.5).
//
//	key = hex(sha256("campaign_id:customer_id:channel"))
//
// Neden bu üçlü:
//   - Aynı kampanyada aynı kişiye aynı kanaldan tek mesaj gitmeli. Anahtar
//     tam olarak bu kimliği kodlar; uq_delivery_key constraint'i de bunu DB
//     seviyesinde garanti eder (uq_delivery ile aynı kümeyi tanımlar).
//   - İçerik revizyonu anahtara girmez: onaydan sonra içerik değişmez, bu
//     yüzden revizyon eklemek yalnızca "aynı kişiye ikinci mesaj" kapısını
//     aralardı.
//   - Deterministik: worker her denemede (ve çöküp başka bir claim
//     aldığında) aynı anahtarı gönderir; provider eski makbuzu döner.
//   - Hash: anahtar dışarı gider; ham ID'leri sızdırmaz ve sabit uzunlukta.
//     Hash'in gizlilik iddiası yok (ID uzayı küçük), amaç format ve kararlılık.
func IdempotencyKey(campaignID, customerID int64, channel string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", campaignID, customerID, channel)))
	return hex.EncodeToString(sum[:])
}
