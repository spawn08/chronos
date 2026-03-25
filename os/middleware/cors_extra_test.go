package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORS_Preflight_NoMaxAge(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.MaxAge = 0
	handler := CORS(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodOptions, "/r", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Max-Age") != "" {
		t.Errorf("did not expect Max-Age when cfg.MaxAge is 0, got %q", rec.Header().Get("Access-Control-Max-Age"))
	}
}

func TestCORS_ExposeHeadersSet(t *testing.T) {
	cfg := CORSConfig{
		AllowOrigins:  []string{"*"},
		AllowMethods:  []string{"GET"},
		ExposeHeaders: []string{"X-Request-Id", "X-Trace"},
	}
	handler := CORS(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	ex := rec.Header().Get("Access-Control-Expose-Headers")
	if !strings.Contains(ex, "X-Request-Id") || !strings.Contains(ex, "X-Trace") {
		t.Errorf("expected expose headers, got %q", ex)
	}
}
