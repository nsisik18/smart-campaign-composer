package delivery

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

// Pacer kanal başına hız sınırıdır (BLUEPRINT §6.3).
//
// rate.NewLimiter(r, 1): burst 1. Klasik büyük burst'lü limiter boşta
// biriken token'larla bir saniye içinde r'yi aşan patlamaya izin verir;
// burst 1 ile iki istek BAŞLANGICI arasında en az 1/r (100/s için 10 ms)
// olur, yani herhangi bir kayan 1 s penceresinde en fazla r başlangıç.
//
// Saat ve uyku enjekte edilir: ReserveN(now) / DelayFrom(now) gerçek saate
// bakmadan çalışır, böylece RATE-01 testi sahte saatle, beklemeden koşar.
type Pacer struct {
	lim   *rate.Limiter
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// NewPacer perSecond sözleşme sınırıdır (100). margin bir güvenlik payıdır
// (örn. 0.03): limiter perSecond*(1-margin) hızında çalışır.
//
// Neden pay: burst 1 limiter başlangıçları tam 1/r aralıkla verir, yani
// r+1 başlangıç tam olarak 1 s'ye yayılır. Bırakılan goroutine'in uyanması
// (scheduler, GC) birkaç yüz mikrosaniye bile gecikirse, DIŞARIDAN ölçen
// biri (provider) aynı 1 s penceresinde r+1 istek görür. Uçtan uca testte
// pay olmadan tutarlı biçimde 101 ölçüldü. %3 pay ile pencerede en fazla
// ~98 başlangıç olur ve ~20 ms'lik oynama tolere edilir; bedeli %3 verim.
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
