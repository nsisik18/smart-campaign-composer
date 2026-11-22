package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/insider-case/smart-campaign-composer/internal/platform"
)

func TestLiveAndMetricsWithoutDB(t *testing.T) {
	// /health/live DB'ye bakmamalı; DB nil iken bile 200 dönmeli.
	h := NewRouter(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Registry: platform.NewRegistry()})
	for _, path := range []string{"/health/live", "/metrics"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), `http_request_duration_seconds_count{code="200",route="/health/live"}`) {
		t.Fatalf("route-pattern label missing:\n%s", rec.Body.String())
	}
}

func TestUnknownAPIRouteUsesErrorFormat(t *testing.T) {
	h := NewRouter(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Registry: platform.NewRegistry()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nope", nil))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
