package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

// newTenantStore returns a migrated in-memory SQLite store for tenant tests.
func newTenantStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s
}

// seedTenant writes one session, trace, event and checkpoint under the given
// tenant. All rows share the same agent id across tenants so that the tenant
// scope — not the agent — is the only thing separating them.
func seedTenant(t *testing.T, s *Store, tenant, sessionID string) {
	t.Helper()
	ctx := storage.WithTenant(context.Background(), tenant)
	now := time.Now()
	if err := s.CreateSession(ctx, &storage.Session{ID: sessionID, AgentID: "agent-shared", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateSession(%s): %v", tenant, err)
	}
	if err := s.InsertTrace(ctx, &storage.Trace{ID: "trace-" + tenant, SessionID: sessionID, Name: "n", Kind: "node", StartedAt: now}); err != nil {
		t.Fatalf("InsertTrace(%s): %v", tenant, err)
	}
	if err := s.AppendEvent(ctx, &storage.Event{ID: "evt-" + tenant, SessionID: sessionID, SeqNum: 1, Type: "node_enter", CreatedAt: now}); err != nil {
		t.Fatalf("AppendEvent(%s): %v", tenant, err)
	}
	if err := s.SaveCheckpoint(ctx, &storage.Checkpoint{ID: "cp-" + tenant, SessionID: sessionID, RunID: "run", NodeID: "node", State: map[string]any{"k": tenant}, SeqNum: 1, CreatedAt: now}); err != nil {
		t.Fatalf("SaveCheckpoint(%s): %v", tenant, err)
	}
}

// TestTenantIsolation proves two tenants sharing the same agent id on one store
// never observe each other's rows, and that id-addressed reads for another
// tenant's objects resolve to not-found even when the caller knows the id
// (closing the IDOR).
func TestTenantIsolation(t *testing.T) {
	s := newTenantStore(t)
	const sessionA, sessionB = "sess-a", "sess-b"
	seedTenant(t, s, "tenant-a", sessionA)
	seedTenant(t, s, "tenant-b", sessionB)

	ctxA := storage.WithTenant(context.Background(), "tenant-a")
	ctxB := storage.WithTenant(context.Background(), "tenant-b")

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"ListSessions is tenant-scoped", func(t *testing.T) {
			for _, tc := range []struct {
				ctx     context.Context
				tenant  string
				session string
			}{{ctxA, "tenant-a", sessionA}, {ctxB, "tenant-b", sessionB}} {
				got, err := s.ListSessions(tc.ctx, "agent-shared", 50, 0)
				if err != nil {
					t.Fatalf("ListSessions: %v", err)
				}
				if len(got) != 1 {
					t.Fatalf("%s: got %d sessions, want 1", tc.tenant, len(got))
				}
				if got[0].TenantID != tc.tenant || got[0].ID != tc.session {
					t.Errorf("%s: leaked session %+v", tc.tenant, got[0])
				}
			}
		}},
		{"GetSession rejects cross-tenant id (IDOR)", func(t *testing.T) {
			// tenant-a knows tenant-b's session id but must not reach it.
			if _, err := s.GetSession(ctxA, sessionB); err == nil {
				t.Error("GetSession(A, sess-b) succeeded; IDOR not closed")
			}
			if sess, err := s.GetSession(ctxA, sessionA); err != nil || sess.TenantID != "tenant-a" {
				t.Fatalf("GetSession(A, sess-a): sess=%v err=%v", sess, err)
			}
		}},
		{"ListTraces cannot cross tenants", func(t *testing.T) {
			// tenant-a asks for tenant-b's session id: it gets nothing.
			cross, _ := s.ListTraces(ctxA, sessionB)
			if len(cross) != 0 {
				t.Errorf("tenant-a saw %d traces of tenant-b's session", len(cross))
			}
			own, _ := s.ListTraces(ctxA, sessionA)
			if len(own) != 1 || own[0].ID != "trace-tenant-a" {
				t.Errorf("tenant-a own traces = %+v", own)
			}
		}},
		{"GetTrace rejects cross-tenant id (IDOR)", func(t *testing.T) {
			if _, err := s.GetTrace(ctxA, "trace-tenant-b"); err == nil {
				t.Error("GetTrace(A, tenant-b trace) succeeded; IDOR not closed")
			}
			if got, err := s.GetTrace(ctxB, "trace-tenant-b"); err != nil || got.TenantID != "tenant-b" {
				t.Errorf("GetTrace(B): got=%v err=%v", got, err)
			}
		}},
		{"GetCheckpoint rejects cross-tenant id (IDOR)", func(t *testing.T) {
			if _, err := s.GetCheckpoint(ctxA, "cp-tenant-b"); err == nil {
				t.Error("GetCheckpoint(A, tenant-b cp) succeeded; IDOR not closed")
			}
			if got, err := s.GetCheckpoint(ctxB, "cp-tenant-b"); err != nil || got.State["k"] != "tenant-b" {
				t.Errorf("GetCheckpoint(B): got=%v err=%v", got, err)
			}
		}},
		{"GetLatestCheckpoint cannot cross tenants", func(t *testing.T) {
			if _, err := s.GetLatestCheckpoint(ctxA, sessionB); err == nil {
				t.Error("GetLatestCheckpoint(A, sess-b) succeeded; IDOR not closed")
			}
			a, err := s.GetLatestCheckpoint(ctxA, sessionA)
			if err != nil || a.State["k"] != "tenant-a" {
				t.Errorf("latest(A) = %v, err=%v", a, err)
			}
		}},
		{"ListEvents cannot cross tenants", func(t *testing.T) {
			cross, _ := s.ListEvents(ctxA, sessionB, 0)
			if len(cross) != 0 {
				t.Errorf("tenant-a saw %d events of tenant-b's session", len(cross))
			}
			own, _ := s.ListEvents(ctxA, sessionA, 0)
			if len(own) != 1 || own[0].ID != "evt-tenant-a" {
				t.Errorf("tenant-a own events = %+v", own)
			}
		}},
		{"default tenant sees neither", func(t *testing.T) {
			got, err := s.ListSessions(context.Background(), "agent-shared", 50, 0)
			if err != nil {
				t.Fatalf("ListSessions(default): %v", err)
			}
			if len(got) != 0 {
				t.Errorf("default tenant saw %d sessions, want 0", len(got))
			}
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}
