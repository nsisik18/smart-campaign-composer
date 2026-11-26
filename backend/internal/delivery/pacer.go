package delivery

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

// Pacer kanal başına hız sınırıdır. Burst 1: iki istek başlangıcı arasında
// en az 1/r süre olur, böylece hiçbir 1 saniyelik pencerede r'yi aşmaz.
// Büyük burst'lü limiter boşta biriken token'larla bunu aşabilirdi.
// Saat ve uyku testte değiştirilir.
type Pacer struct {
	lim   *rate.Limiter
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// NewPacer limiter'ı perSecond*(1-margin) hızında kurar. Pay olmadan
// goroutine uyanma gecikmesi yüzünden provider tarafında 101/s ölçüldü;
// %3 pay bunu emer (ADR-8).
func NewPacer(perSecond int, margin float64) *Pacer {
	effective := float64(perSecond) * (1 - margin)
	return &Pacer{
		lim:   rate.NewLimiter(rate.Limit(effective), 1),
		now:   time.Now,
		sleep: sleepCtx,
	}
}

// Wait bir sonraki istek başlangıç anına kadar bekler. ctx iptal edilirse
// rezervasyon geri verilir ki iptal edilen istek slot yemesin.
func (p *Pacer) Wait(ctx context.Context) error {
	now := p.now()
	r := p.lim.ReserveN(now, 1)
	if !r.OK() {
		return context.Canceled // burst 1 ile n=1 her zaman OK; savunma
	}
	if err := p.sleep(ctx, r.DelayFrom(now)); err != nil {
		r.CancelAt(p.now())
		return err
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
