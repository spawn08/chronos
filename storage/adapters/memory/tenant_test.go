package memory

import (
	"context"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

// TestTenantIsolation proves the in-memory adapter scopes every read and write
// to the context tenant: two tenants sharing an agent and session id never see
// each other's rows, and id-addressed reads for another tenant's objects
// resolve to not-found (closing the IDOR).
func TestTenantIsolation(t *testing.T) {
	s := New()
	const sharedSession = "sess-shared"
	now := time.Now()

	seed := func(tenant string) {
		ctx := storage.WithTenant(context.Background(), tenant)
		if err := s.CreateSession(ctx, &storage.Session{ID: sharedSession + "-" + tenant, AgentID: "agent-shared", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("CreateSession(%s): %v", tenant, err)
		}
		if err := s.InsertTrace(ctx, &storage.Trace{ID: "trace-" + tenant, SessionID: sharedSession, Name: "n", Kind: "node", StartedAt: now}); err != nil {
			t.Fatalf("InsertTrace(%s): %v", tenant, err)
		}
		if err := s.SaveCheckpoint(ctx, &storage.Checkpoint{ID: "cp-" + tenant, SessionID: sharedSession, RunID: "r", NodeID: "n", State: map[string]any{"k": tenant}, SeqNum: 1, CreatedAt: now}); err != nil {
			t.Fatalf("SaveCheckpoint(%s): %v", tenant, err)
		}
	}
	seed("tenant-a")
	seed("tenant-b")

	ctxA := storage.WithTenant(context.Background(), "tenant-a")
	ctxB := storage.WithTenant(context.Background(), "tenant-b")

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"ListSessions scoped", func(t *testing.T) {
			a, _ := s.ListSessions(ctxA, "agent-shared", 50, 0)
			b, _ := s.ListSessions(ctxB, "agent-shared", 50, 0)
			if len(a) != 1 || a[0].TenantID != "tenant-a" {
				t.Errorf("A sessions = %+v", a)
			}
			if len(b) != 1 || b[0].TenantID != "tenant-b" {
				t.Errorf("B sessions = %+v", b)
			}
		}},
		{"ListTraces scoped", func(t *testing.T) {
			a, _ := s.ListTraces(ctxA, sharedSession)
			if len(a) != 1 || a[0].ID != "trace-tenant-a" {
				t.Errorf("A traces = %+v", a)
			}
		}},
		{"GetTrace cross-tenant not found (IDOR)", func(t *testing.T) {
			if _, err := s.GetTrace(ctxA, "trace-tenant-b"); err == nil {
				t.Error("GetTrace(A, tenant-b) succeeded; IDOR not closed")
			}
		}},
		{"GetCheckpoint cross-tenant not found (IDOR)", func(t *testing.T) {
			if _, err := s.GetCheckpoint(ctxA, "cp-tenant-b"); err == nil {
				t.Error("GetCheckpoint(A, tenant-b) succeeded; IDOR not closed")
			}
			if got, err := s.GetCheckpoint(ctxB, "cp-tenant-b"); err != nil || got.State["k"] != "tenant-b" {
				t.Errorf("GetCheckpoint(B) = %v, err=%v", got, err)
			}
		}},
		{"default tenant isolated", func(t *testing.T) {
			got, _ := s.ListSessions(context.Background(), "agent-shared", 50, 0)
			if len(got) != 0 {
				t.Errorf("default tenant saw %d sessions, want 0", len(got))
			}
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}
