package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_Preflight(t *testing.T) {
	handler := CORS(DefaultCORSConfig())(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("OPTIONS", "/api/test", http.NoBody)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("got status %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("got allow-origin=%q, want *", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("missing allow-methods header")
	}
}

func TestCORS_RestrictedOrigins(t *testing.T) {
	cfg := CORSConfig{
		AllowOrigins: []string{"https://example.com"},
		AllowMethods: []string{"GET"},
	}
	handler := CORS(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("got allow-origin=%q, want https://example.com", got)
	}

	req2 := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req2.Header.Set("Origin", "https://evil.com")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if got := rec2.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("evil origin should not get allow-origin, got %q", got)
	}
}

func TestCORS_Credentials(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowCredentials = true
	handler := CORS(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("got credentials=%q, want true", got)
	}
}

func TestCORS_RegularRequest(t *testing.T) {
	handler := CORS(DefaultCORSConfig())(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("hello"))
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}
	if rec.Body.String() != "hello" {
		t.Errorf("got body=%q, want hello", rec.Body.String())
	}
}
