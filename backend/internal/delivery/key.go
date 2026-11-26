// Package delivery gönderim tarafıdır: claim, hız sınırı, retry ve provider çağrısı.
package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// IdempotencyKey provider'a giden anahtardır: sha256("campaign:customer:channel").
// Her denemede aynıdır, böylece worker çöküp tekrar gönderse de provider ilk
// makbuzu döner. İçerik revizyonu bilerek yok: onaydan sonra içerik değişmez.
func IdempotencyKey(campaignID, customerID int64, channel string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", campaignID, customerID, channel)))
	return hex.EncodeToString(sum[:])
}
