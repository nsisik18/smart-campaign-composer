package platform

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Serve HTTP sunucusunu çalıştırır, ctx iptal edilince 10 s içinde
// graceful shutdown yapar. ReadTimeout bilerek yok: SSE uzun yaşar.
func Serve(ctx context.Context, addr string, h http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	log.Info("http shutting down")
	return srv.Shutdown(shCtx)
}
