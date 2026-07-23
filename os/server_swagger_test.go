package chronosos

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spawn08/chronos/os/auth"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

// TestSwaggerDocJSON verifies the generated OpenAPI document is served at
// /swagger/doc.json and parses as JSON carrying an openapi/swagger version.
func TestSwaggerDocJSON(t *testing.T) {
	s := New(":0", memory.New())

	req := httptest.NewRequest(http.MethodGet, "/swagger/doc.json", http.NoBody)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /swagger/doc.json: got %d, want 200 (body=%q)", w.Code, w.Body.String())
	}

	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("doc.json is not valid JSON: %v", err)
	}
	if spec["swagger"] == nil && spec["openapi"] == nil {
		t.Fatalf("doc.json missing swagger/openapi version field: %v", spec)
	}
	if _, ok := spec["paths"]; !ok {
		t.Errorf("doc.json missing paths object")
	}
}

// TestSwaggerUIRoute verifies the Swagger UI index is served.
func TestSwaggerUIRoute(t *testing.T) {
	s := New(":0", memory.New())

	req := httptest.NewRequest(http.MethodGet, "/swagger/index.html", http.NoBody)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /swagger/index.html: got %d, want 200", w.Code)
	}
}

// TestSwaggerBypassDoesNotLeakViaTraversal verifies that a path-traversal
// disguised as a Swagger asset (/swagger/../api/sessions) does NOT ride the
// auth bypass: after canonicalization it targets a protected route and must be
// challenged for authentication rather than served.
func TestSwaggerBypassDoesNotLeakViaTraversal(t *testing.T) {
	const secret = "test-secret"
	s := NewWithOptions(":0", memory.New(), WithJWTAuth(auth.JWTConfig{Secret: secret}))

	req := httptest.NewRequest(http.MethodGet, "/swagger/../api/sessions", http.NoBody)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("traversal /swagger/../api/sessions was served (status 200) — auth bypass leak: body=%q", w.Body.String())
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("traversal request: got %d, want 401 (challenged for auth)", w.Code)
	}
}

// TestSwaggerDisabled verifies WithSwagger(false) removes the UI/spec routes.
func TestSwaggerDisabled(t *testing.T) {
	s := NewWithOptions(":0", memory.New(), WithSwagger(false))

	for _, path := range []string{"/swagger/doc.json", "/swagger/index.html", "/swagger/"} {
		req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			t.Errorf("GET %s with swagger disabled: got 200, want not-found", path)
		}
	}
}

// TestRBACEnforcement verifies method-based role gating on /api/* when
// WithRBAC is enabled alongside authentication.
func TestRBACEnforcement(t *testing.T) {
	const secret = "test-secret"
	s := NewWithOptions(":0", memory.New(),
		WithJWTAuth(auth.JWTConfig{Secret: secret}),
		WithRBAC(true),
	)

	tok := func(roles ...string) string {
		return "Bearer " + auth.CreateTestToken(auth.UserClaims{
			UserID: "u1",
			Roles:  roles,
			Exp:    time.Now().Add(time.Hour).Unix(),
		}, secret)
	}

	tests := []struct {
		name       string
		method     string
		path       string
		authHeader string
		wantStatus int
	}{
		{"viewer may read", http.MethodGet, "/api/sessions", tok("viewer"), http.StatusOK},
		{"viewer denied write", http.MethodPost, "/api/schedules", tok("viewer"), http.StatusForbidden},
		{"unauthenticated challenged", http.MethodGet, "/api/sessions", "", http.StatusUnauthorized},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("%s %s: got %d, want %d (body=%q)", tc.method, tc.path, w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}

	// A user-role principal passes the RBAC gate on a write (the handler may
	// then reject the empty body, but never with 401/403).
	req := httptest.NewRequest(http.MethodPost, "/api/schedules", http.NoBody)
	req.Header.Set("Authorization", tok("user"))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
		t.Errorf("user-role write blocked by RBAC: got %d, want it to pass the gate", w.Code)
	}
}

// TestSwaggerBypassesAuth verifies that with JWT auth enabled the Swagger
// endpoints remain reachable while protected API routes require a token.
func TestSwaggerBypassesAuth(t *testing.T) {
	const secret = "test-secret"
	s := NewWithOptions(":0", memory.New(), WithJWTAuth(auth.JWTConfig{Secret: secret}))

	tests := []struct {
		name       string
		path       string
		authHeader string
		wantStatus int
	}{
		{"doc.json bypasses auth", "/swagger/doc.json", "", http.StatusOK},
		{"ui bypasses auth", "/swagger/index.html", "", http.StatusOK},
		{"api requires token", "/api/sessions", "", http.StatusUnauthorized},
		{
			"api with valid token",
			"/api/sessions",
			"Bearer " + auth.CreateTestToken(auth.UserClaims{UserID: "u1", Exp: time.Now().Add(time.Hour).Unix()}, secret),
			http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, http.NoBody)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("GET %s: got %d, want %d (body=%q)", tc.path, w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}
