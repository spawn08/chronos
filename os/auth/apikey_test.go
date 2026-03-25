package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIKeyMiddleware_ValidKey(t *testing.T) {
	cfg := APIKeyConfig{
		Keys: map[string]APIKeyEntry{
			"test-key-123": {Scope: "admin", UserID: "api-user-1"},
		},
	}

	handler := APIKeyMiddleware(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFromContext(r.Context())
			if !ok {
				t.Error("user not in context")
				return
			}
			if u.UserID != "api-user-1" {
				t.Errorf("got user_id=%q, want api-user-1", u.UserID)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req.Header.Set("X-Api-Key", "test-key-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}
}

func TestAPIKeyMiddleware_MissingKey(t *testing.T) {
	cfg := APIKeyConfig{Keys: map[string]APIKeyEntry{"key": {Scope: "admin"}}}
	handler := APIKeyMiddleware(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

func TestAPIKeyMiddleware_InvalidKey(t *testing.T) {
	cfg := APIKeyConfig{Keys: map[string]APIKeyEntry{"valid-key": {Scope: "admin"}}}
	handler := APIKeyMiddleware(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req.Header.Set("X-Api-Key", "wrong-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

func TestAPIKeyMiddleware_CustomHeader(t *testing.T) {
	cfg := APIKeyConfig{
		HeaderName: "X-Custom-Key",
		Keys:       map[string]APIKeyEntry{"my-key": {Scope: "user", UserID: "u1"}},
	}
	handler := APIKeyMiddleware(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req.Header.Set("X-Custom-Key", "my-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}
}

func TestAPIKeyMiddleware_SkipPaths(t *testing.T) {
	cfg := APIKeyConfig{
		Keys:      map[string]APIKeyEntry{"key": {Scope: "admin"}},
		SkipPaths: []string{"/health"},
	}
	handler := APIKeyMiddleware(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/health", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("health should skip auth, got status %d", rec.Code)
	}
}
