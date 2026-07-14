package chronosos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/os/auth"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

// TestDefaultNoAuth confirms the safe default: with no auth option, protected
// routes are reachable (backward compatible with existing behavior).
func TestDefaultNoAuth(t *testing.T) {
	s := New(":0", memory.New())

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", http.NoBody)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with default no-auth, got %d", w.Code)
	}
}

func TestAPIKeyAuthEnforcement(t *testing.T) {
	const key = "secret-key-123"
	s := NewWithOptions(":0", memory.New(), WithAPIKeyAuth(auth.APIKeyConfig{
		HeaderName: "X-Api-Key",
		Keys:       map[string]auth.APIKeyEntry{key: {Scope: "admin", UserID: "u1"}},
	}))

	tests := []struct {
		name       string
		header     string
		value      string
		wantStatus int
	}{
		{"missing credentials", "", "", http.StatusUnauthorized},
		{"invalid credentials", "X-Api-Key", "wrong", http.StatusUnauthorized},
		{"valid credentials", "X-Api-Key", key, http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/sessions", http.NoBody)
			if tc.header != "" {
				req.Header.Set(tc.header, tc.value)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("got %d, want %d (body=%q)", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

func TestJWTAuthEnforcement(t *testing.T) {
	const secret = "test-secret"
	s := NewWithOptions(":0", memory.New(), WithJWTAuth(auth.JWTConfig{Secret: secret}))
	valid := auth.CreateTestToken(auth.UserClaims{UserID: "u1", Roles: []string{"admin"}, Exp: time.Now().Add(time.Hour).Unix()}, secret)

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{"missing token", "", http.StatusUnauthorized},
		{"invalid token", "Bearer not.a.jwt", http.StatusUnauthorized},
		{"valid token", "Bearer " + valid, http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/sessions", http.NoBody)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("got %d, want %d (body=%q)", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

// TestAuthSkipsHealthEndpoints ensures probes/metrics stay reachable when auth
// is enabled.
func TestAuthSkipsHealthEndpoints(t *testing.T) {
	s := NewWithOptions(":0", memory.New(), WithJWTAuth(auth.JWTConfig{Secret: "s"}))
	s.SetReady(true)

	for _, path := range []string{"/healthz", "/health", "/health/live", "/health/ready"} {
		req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200 without auth, got %d", path, w.Code)
		}
	}
}

func TestServerTimeoutsConfigured(t *testing.T) {
	s := New(":0", memory.New())
	srv := s.httpServer()

	if srv.ReadTimeout != defaultReadTimeout {
		t.Errorf("ReadTimeout = %s, want %s", srv.ReadTimeout, defaultReadTimeout)
	}
	if srv.ReadHeaderTimeout != defaultReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %s, want %s", srv.ReadHeaderTimeout, defaultReadHeaderTimeout)
	}
	if srv.WriteTimeout != defaultWriteTimeout {
		t.Errorf("WriteTimeout = %s, want %s", srv.WriteTimeout, defaultWriteTimeout)
	}
	if srv.IdleTimeout != defaultIdleTimeout {
		t.Errorf("IdleTimeout = %s, want %s", srv.IdleTimeout, defaultIdleTimeout)
	}
	if srv.MaxHeaderBytes != defaultMaxHeaderBytes {
		t.Errorf("MaxHeaderBytes = %d, want %d", srv.MaxHeaderBytes, defaultMaxHeaderBytes)
	}
}

func TestOversizedBodyRejected(t *testing.T) {
	s := NewWithOptions(":0", memory.New(), WithMaxBodyBytes(16))

	big := `{"agent_id":"` + strings.Repeat("a", 1024) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/schedules", strings.NewReader(big))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("got %d, want 413 (body=%q)", w.Code, w.Body.String())
	}
}

// TestPanicRecovery proves a panicking handler yields 500 and the server (the
// handler chain) survives to serve the next request.
func TestPanicRecovery(t *testing.T) {
	s := New(":0", memory.New())
	s.mux.HandleFunc("/boom", func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	})
	h := s.Handler()

	req := httptest.NewRequest(http.MethodGet, "/boom", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", w.Code)
	}

	// The chain must still handle a subsequent request.
	req2 := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("server did not survive panic: got %d for /healthz", w2.Code)
	}
}

// TestReadinessDoesNotMigrate proves the readiness handler is cheap and never
// calls Store.Migrate.
func TestReadinessDoesNotMigrate(t *testing.T) {
	store := &countingMigrateStore{Store: memory.New()}
	s := New(":0", store)
	s.SetReady(true)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("iteration %d: got %d, want 200", i, w.Code)
		}
	}
	if store.migrateCalls != 0 {
		t.Errorf("readiness triggered Migrate %d times, want 0", store.migrateCalls)
	}
}

type countingMigrateStore struct {
	*memory.Store
	migrateCalls int
}

func (s *countingMigrateStore) Migrate(ctx context.Context) error {
	s.migrateCalls++
	return s.Store.Migrate(ctx)
}
