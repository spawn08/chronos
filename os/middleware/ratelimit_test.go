package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimit_AllowsWithinLimit(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerWindow: 5,
		Window:            time.Minute,
		KeyFunc:           func(_ *http.Request) string { return "test" },
	}
	handler := RateLimit(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", http.NoBody)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: got status %d, want 200", i+1, rec.Code)
		}
	}
}

func TestRateLimit_BlocksExcess(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerWindow: 2,
		Window:            time.Minute,
		KeyFunc:           func(_ *http.Request) string { return "test" },
	}
	handler := RateLimit(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", http.NoBody)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("got status %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("got remaining=%q, want 0", got)
	}
}

func TestRateLimit_DifferentKeys(t *testing.T) {
	callCount := 0
	cfg := RateLimitConfig{
		RequestsPerWindow: 1,
		Window:            time.Minute,
		KeyFunc: func(r *http.Request) string {
			return r.Header.Get("X-Client")
		},
	}
	handler := RateLimit(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount++
			w.WriteHeader(http.StatusOK)
		}),
	)

	req1 := httptest.NewRequest("GET", "/test", http.NoBody)
	req1.Header.Set("X-Client", "client-a")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest("GET", "/test", http.NoBody)
	req2.Header.Set("X-Client", "client-b")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if callCount != 2 {
		t.Errorf("got %d calls, want 2 (different keys)", callCount)
	}
}

func TestRateLimit_SetsHeaders(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerWindow: 10,
		Window:            time.Minute,
		KeyFunc:           func(_ *http.Request) string { return "test" },
	}
	handler := RateLimit(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-RateLimit-Limit"); got != "10" {
		t.Errorf("got limit=%q, want 10", got)
	}
	if got := rec.Header().Get("X-RateLimit-Remaining"); got != "9" {
		t.Errorf("got remaining=%q, want 9", got)
	}
	if got := rec.Header().Get("X-RateLimit-Reset"); got == "" {
		t.Error("missing reset header")
	}
}

func TestIPKeyFunc(t *testing.T) {
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"
	if got := IPKeyFunc(req); got != "192.168.1.1" {
		t.Errorf("got %q, want 192.168.1.1", got)
	}

	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	if got := IPKeyFunc(req); got != "10.0.0.1" {
		t.Errorf("got %q, want 10.0.0.1", got)
	}
}

func TestDefaultRateLimitConfig(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	if cfg.RequestsPerWindow != 100 {
		t.Errorf("RequestsPerWindow=%d, want 100", cfg.RequestsPerWindow)
	}
	if cfg.Window != time.Minute {
		t.Errorf("Window=%v, want 1m", cfg.Window)
	}
	if cfg.KeyFunc == nil {
		t.Error("KeyFunc should not be nil")
	}
}
