package platform

import "time"

// Clock test edilebilir zaman. Segment derlemesi ve (Gün 5) backoff bunu kullanır.
type Clock interface{ Now() time.Time }

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

// FixedClock testler için.
type FixedClock struct{ T time.Time }

func (c FixedClock) Now() time.Time { return c.T }
