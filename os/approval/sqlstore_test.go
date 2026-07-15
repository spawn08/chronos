package approval

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// sharedDSN returns a WAL-mode SQLite DSN so multiple independent *sql.DB
// handles (simulating distributed replicas) share one file.
func sharedDSN(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "approval.db")
	return path + "?_busy_timeout=5000&_journal_mode=WAL&_txlock=immediate"
}

func openApprovalStore(t *testing.T, dsn string) *SQLStore {
	t.Helper()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	return NewSQLStore(db, DialectSQLite)
}

func TestSQLStore_CreateResolveGet(t *testing.T) {
	store := openApprovalStore(t, sharedDSN(t))
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tests := []struct {
		name       string
		approved   bool
		wantStatus string
	}{
		{"approve", true, StatusApproved},
		{"deny", false, StatusDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &Request{ID: "id_" + tt.name, ToolName: "tool", Args: map[string]any{"k": "v"}, Status: StatusPending, CreatedAt: time.Now()}
			if err := store.Create(ctx, req); err != nil {
				t.Fatalf("create: %v", err)
			}
			got, err := store.Resolve(ctx, req.ID, tt.approved, time.Now())
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			reloaded, err := store.Get(ctx, req.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if reloaded.Status != tt.wantStatus {
				t.Fatalf("reloaded status = %q, want %q", reloaded.Status, tt.wantStatus)
			}
			if reloaded.Args["k"] != "v" {
				t.Fatalf("args not round-tripped: %#v", reloaded.Args)
			}
		})
	}
}

func TestSQLStore_ResolveNotFound(t *testing.T) {
	store := openApprovalStore(t, sharedDSN(t))
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, err := store.Resolve(context.Background(), "missing", true, time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSQLStore_ListPendingOnly(t *testing.T) {
	store := openApprovalStore(t, sharedDSN(t))
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if err := store.Create(ctx, &Request{ID: id, ToolName: "t", Status: StatusPending, CreatedAt: time.Now()}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	if _, err := store.Resolve(ctx, "b", true, time.Now()); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("pending = %d, want 2 (resolved excluded)", len(list))
	}
}

// TestService_StoreBacked_CrossReplica proves a decision recorded on one
// replica's Service (writing to the shared store) unblocks a waiter running on a
// different Service instance backed by the same store — the persistence /
// cross-replica requirement. Run with -race.
func TestService_StoreBacked_CrossReplica(t *testing.T) {
	dsn := sharedDSN(t)
	storeA := openApprovalStore(t, dsn)
	if err := storeA.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	storeB := openApprovalStore(t, dsn)

	svcA := NewService(WithStore(storeA), WithPollInterval(10*time.Millisecond))
	svcB := NewService(WithStore(storeB), WithPollInterval(10*time.Millisecond))

	done := make(chan bool, 1)
	go func() {
		approved, err := svcA.RequestApproval(context.Background(), "deploy", nil)
		if err != nil {
			t.Errorf("RequestApproval: %v", err)
		}
		done <- approved
	}()

	// Find the request ID via svcA's live waiter, then respond through svcB, which
	// only shares the durable store (no in-process channel to svcA).
	id := waitForPending(t, svcA)
	body, _ := json.Marshal(map[string]any{"id": id, "approved": true})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/respond", bytes.NewBuffer(body))
	svcB.HandleRespond(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("svcB respond code = %d, want 200", w.Code)
	}

	select {
	case approved := <-done:
		if !approved {
			t.Fatal("expected approved=true across replicas")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("waiter did not observe cross-replica decision")
	}
}
