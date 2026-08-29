package chronosos

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/os/auth"
	"github.com/spawn08/chronos/os/dashboard"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

func testGraph(t *testing.T) *graph.CompiledGraph {
	t.Helper()
	g := graph.New("wf").
		AddNode("a", func(_ context.Context, s graph.State) (graph.State, error) { return s, nil }).
		SetEntryPoint("a").
		SetFinishPoint("a")
	cg, err := g.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return cg
}

// TestDashboardUIBypassesAuth mirrors TestSwaggerBypassesAuth: the static
// dashboard shell is reachable without a token (so one can be entered from
// the page itself), while /api/dashboard/* stays behind the normal auth
// chain like every other /api/ route.
func TestDashboardUIBypassesAuth(t *testing.T) {
	const secret = "test-secret"
	s := NewWithOptions(":0", memory.New(), WithJWTAuth(auth.JWTConfig{Secret: secret}))

	tests := []struct {
		name       string
		path       string
		authHeader string
		wantStatus int
	}{
		{"ui shell bypasses auth", "/dashboard/", "", http.StatusOK},
		{"ui redirect bypasses auth", "/dashboard", "", http.StatusMovedPermanently},
		{"api requires token", "/api/dashboard/checkpoints?session_id=x", "", http.StatusUnauthorized},
		{
			"api with valid token",
			"/api/dashboard/checkpoints?session_id=x",
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

// TestDashboardBypassDoesNotLeakViaTraversal mirrors
// TestSwaggerBypassDoesNotLeakViaTraversal: a traversal path disguised as a
// dashboard asset must not ride the auth bypass to a protected route.
func TestDashboardBypassDoesNotLeakViaTraversal(t *testing.T) {
	const secret = "test-secret"
	s := NewWithOptions(":0", memory.New(), WithJWTAuth(auth.JWTConfig{Secret: secret}))

	req := httptest.NewRequest(http.MethodGet, "/dashboard/../api/sessions", http.NoBody)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("traversal /dashboard/../api/sessions was served (status 200) — auth bypass leak: body=%q", w.Body.String())
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("traversal request: got %d, want 401 (challenged for auth)", w.Code)
	}
}

// TestDashboardDisabled verifies WithDashboard(false) removes the UI and API routes.
func TestDashboardDisabled(t *testing.T) {
	s := NewWithOptions(":0", memory.New(), WithDashboard(false))

	for _, path := range []string{"/dashboard/", "/dashboard", "/api/dashboard/checkpoints?session_id=x"} {
		req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code == http.StatusOK || w.Code == http.StatusMovedPermanently {
			t.Errorf("GET %s with dashboard disabled: got %d, want not-found", path, w.Code)
		}
	}
}

// TestDashboardGraphsAndCostOptions verifies WithGraphs/WithCostTracker wire
// into the server's Dashboard handler and are reachable through the HTTP API.
func TestDashboardGraphsAndCostOptions(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	now := time.Now()
	if err := store.CreateSession(ctx, &storage.Session{ID: "s1", AgentID: "wf", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	s := NewWithOptions(":0", store, WithGraphs(dashboard.GraphRegistry{"wf": testGraph(t)}))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/graph?session_id=s1", http.NoBody)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET graph: status = %d, body=%s", w.Code, w.Body.String())
	}
}

// TestDashboardAPITenantIsolation proves a session/checkpoint created under
// one tenant is invisible via the dashboard API to a caller authenticated as
// another tenant, mirroring TestTenantIsolationHTTP for the pre-existing
// endpoints.
func TestDashboardAPITenantIsolation(t *testing.T) {
	store := memory.New()
	now := time.Now()
	ctxA := storage.WithTenant(context.Background(), "tenant-a")
	if err := store.CreateSession(ctxA, &storage.Session{ID: "sess-a", AgentID: "wf", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCheckpoint(ctxA, &storage.Checkpoint{ID: "cp-a", SessionID: "sess-a", RunID: "r", NodeID: "a", State: map[string]any{}, SeqNum: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	const keyB = "key-b"
	s := NewWithOptions(":0", store, WithAPIKeyAuth(auth.APIKeyConfig{
		HeaderName: "X-Api-Key",
		Keys: map[string]auth.APIKeyEntry{
			keyB: {Scope: "admin", UserID: "ub", TenantID: "tenant-b"},
		},
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/checkpoints?session_id=sess-a", http.NoBody)
	req.Header.Set("X-Api-Key", keyB)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Checkpoints []*storage.Checkpoint `json:"checkpoints"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Checkpoints) != 0 {
		t.Errorf("tenant-b saw %d of tenant-a's checkpoints via the dashboard API, want 0", len(resp.Checkpoints))
	}
}
