package chronosos

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spawn08/chronos/os/auth"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

// seedTenantData writes a session, trace and latest checkpoint under the given
// tenant so the HTTP handlers have per-tenant objects to (fail to) reach.
func seedTenantData(t *testing.T, store storage.Storage, tenant, sessionID string) {
	t.Helper()
	ctx := storage.WithTenant(context.Background(), tenant)
	now := time.Now()
	if err := store.CreateSession(ctx, &storage.Session{ID: sessionID, AgentID: "agent-shared", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateSession(%s): %v", tenant, err)
	}
	if err := store.InsertTrace(ctx, &storage.Trace{ID: "trace-" + tenant, SessionID: sessionID, Name: "n", Kind: "node", StartedAt: now}); err != nil {
		t.Fatalf("InsertTrace(%s): %v", tenant, err)
	}
	if err := store.SaveCheckpoint(ctx, &storage.Checkpoint{ID: "cp-" + tenant, SessionID: sessionID, RunID: "r", NodeID: "n", State: map[string]any{"k": tenant}, SeqNum: 1, CreatedAt: now}); err != nil {
		t.Fatalf("SaveCheckpoint(%s): %v", tenant, err)
	}
}

// TestTenantIsolationHTTP proves the control plane derives the tenant from the
// authenticated principal (not client-supplied ids) and that a request
// authenticated as tenant-a can never read tenant-b's session/trace/checkpoint,
// even when it supplies tenant-b's ids — the read resolves to empty or 404,
// closing the IDOR.
func TestTenantIsolationHTTP(t *testing.T) {
	store := memory.New()
	const sessionA, sessionB = "sess-a", "sess-b"
	seedTenantData(t, store, "tenant-a", sessionA)
	seedTenantData(t, store, "tenant-b", sessionB)

	const keyA, keyB = "key-a", "key-b"
	s := NewWithOptions(":0", store, WithAPIKeyAuth(auth.APIKeyConfig{
		HeaderName: "X-Api-Key",
		Keys: map[string]auth.APIKeyEntry{
			keyA: {Scope: "admin", UserID: "ua", TenantID: "tenant-a"},
			keyB: {Scope: "admin", UserID: "ub", TenantID: "tenant-b"},
		},
	}))

	do := func(key, method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, http.NoBody)
		req.Header.Set("X-Api-Key", key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		return w
	}

	t.Run("ListSessions returns only the caller's tenant", func(t *testing.T) {
		w := do(keyA, http.MethodGet, "/api/sessions?agent_id=agent-shared")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		var resp struct {
			Sessions []*storage.Session `json:"sessions"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Sessions) != 1 {
			t.Fatalf("got %d sessions, want 1", len(resp.Sessions))
		}
		if resp.Sessions[0].TenantID != "tenant-a" || resp.Sessions[0].ID != sessionA {
			t.Errorf("leaked session %+v", resp.Sessions[0])
		}
	})

	t.Run("ListTraces cannot see another tenant's session (IDOR)", func(t *testing.T) {
		// tenant-a supplies tenant-b's session id: it must get nothing back.
		w := do(keyA, http.MethodGet, "/api/traces?session_id="+sessionB)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		var resp struct {
			Traces []*storage.Trace `json:"traces"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Traces) != 0 {
			t.Errorf("tenant-a saw %d traces of tenant-b's session", len(resp.Traces))
		}
	})

	t.Run("session state GET is scoped to caller tenant", func(t *testing.T) {
		w := do(keyA, http.MethodGet, "/api/sessions/state?session_id="+sessionA)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp["checkpoint_id"] != "cp-tenant-a" {
			t.Errorf("checkpoint_id = %v, want cp-tenant-a", resp["checkpoint_id"])
		}
	})

	t.Run("cross-tenant session state is 404 (IDOR)", func(t *testing.T) {
		// tenant-a supplies tenant-b's session id to the state endpoint: the
		// checkpoint lookup is tenant-scoped, so it resolves to not-found (404),
		// never tenant-b's checkpoint.
		w := do(keyA, http.MethodGet, "/api/sessions/state?session_id="+sessionB)
		if w.Code != http.StatusNotFound {
			t.Fatalf("cross-tenant state: status = %d, want 404 (body=%s)", w.Code, w.Body.String())
		}
	})
}
