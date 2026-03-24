package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(":memory:")
	if err != nil {
		t.Fatalf("New(:memory:): %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSessionCRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tests := []struct {
		name string
		sess *storage.Session
	}{
		{
			name: "basic session",
			sess: &storage.Session{
				ID: "s1", AgentID: "a1", Status: "running",
				CreatedAt: time.Now(), UpdatedAt: time.Now(),
			},
		},
		{
			name: "completed session",
			sess: &storage.Session{
				ID: "s2", AgentID: "a1", Status: "completed",
				CreatedAt: time.Now(), UpdatedAt: time.Now(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.CreateSession(ctx, tt.sess); err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			got, err := store.GetSession(ctx, tt.sess.ID)
			if err != nil {
				t.Fatalf("GetSession: %v", err)
			}
			if got.ID != tt.sess.ID || got.AgentID != tt.sess.AgentID {
				t.Fatalf("session mismatch: got %+v", got)
			}
		})
	}

	sessions, err := store.ListSessions(ctx, "a1", 10, 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestMemoryCRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mem := &storage.MemoryRecord{
		ID:      "m1",
		AgentID: "a1",
		Kind:    "long_term",
		Key:     "user_name",
		Value:   "Alice",
	}

	if err := store.PutMemory(ctx, mem); err != nil {
		t.Fatalf("PutMemory: %v", err)
	}

	got, err := store.GetMemory(ctx, "a1", "user_name")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.Value != "Alice" {
		t.Fatalf("expected 'Alice', got %q", got.Value)
	}

	mems, err := store.ListMemory(ctx, "a1", "long_term")
	if err != nil {
		t.Fatalf("ListMemory: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(mems))
	}

	if delErr := store.DeleteMemory(ctx, "m1"); delErr != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}

	mems, err = store.ListMemory(ctx, "a1", "long_term")
	if err != nil {
		t.Fatalf("ListMemory after delete: %v", err)
	}
	if len(mems) != 0 {
		t.Fatalf("expected 0 memories, got %d", len(mems))
	}
}

func TestCheckpointCRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	cp := &storage.Checkpoint{
		ID:        "cp1",
		SessionID: "s1",
		NodeID:    "n1",
		SeqNum:    1,
		State:     map[string]any{"key": "value"},
		CreatedAt: time.Now(),
	}

	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	got, err := store.GetCheckpoint(ctx, "cp1")
	if err != nil {
		t.Fatalf("GetCheckpoint: %v", err)
	}
	if got.NodeID != "n1" {
		t.Fatalf("expected node 'n1', got %q", got.NodeID)
	}

	latest, err := store.GetLatestCheckpoint(ctx, "s1")
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if latest.ID != "cp1" {
		t.Fatalf("expected latest checkpoint 'cp1', got %q", latest.ID)
	}
}

func TestEventCRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	events := []*storage.Event{
		{ID: "e1", SessionID: "s1", Type: "node_enter", SeqNum: 1, Payload: map[string]any{"node": "start"}},
		{ID: "e2", SessionID: "s1", Type: "node_exit", SeqNum: 2, Payload: map[string]any{"node": "start"}},
	}

	for _, e := range events {
		if err := store.AppendEvent(ctx, e); err != nil {
			t.Fatalf("AppendEvent(%s): %v", e.ID, err)
		}
	}

	got, err := store.ListEvents(ctx, "s1", 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}

	afterSeq, err := store.ListEvents(ctx, "s1", 1)
	if err != nil {
		t.Fatalf("ListEvents afterSeq: %v", err)
	}
	if len(afterSeq) != 1 {
		t.Fatalf("expected 1 event after seq 1, got %d", len(afterSeq))
	}
}

func TestTraceCRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tr := &storage.Trace{
		ID:        "t1",
		SessionID: "s1",
		Name:      "test_span",
		Kind:      "node",
		StartedAt: time.Now(),
	}

	if err := store.InsertTrace(ctx, tr); err != nil {
		t.Fatalf("InsertTrace: %v", err)
	}

	got, err := store.GetTrace(ctx, "t1")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if got.Name != "test_span" {
		t.Fatalf("expected span 'test_span', got %q", got.Name)
	}

	traces, err := store.ListTraces(ctx, "s1")
	if err != nil {
		t.Fatalf("ListTraces: %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
}

func TestAuditLogCRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	log := &storage.AuditLog{
		ID:        "al1",
		SessionID: "s1",
		Actor:     "user-1",
		Action:    "tool_call",
		Resource:  "calculator",
		Detail:    map[string]any{"args": map[string]any{"x": 42}},
		CreatedAt: time.Now(),
	}

	if err := store.AppendAuditLog(ctx, log); err != nil {
		t.Fatalf("AppendAuditLog: %v", err)
	}

	logs, err := store.ListAuditLogs(ctx, "s1", 10, 0)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(logs))
	}
	if logs[0].Actor != "user-1" {
		t.Errorf("Actor = %q, want user-1", logs[0].Actor)
	}
	if logs[0].Action != "tool_call" {
		t.Errorf("Action = %q, want tool_call", logs[0].Action)
	}
}

func TestSessionUpdate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := &storage.Session{
		ID: "su1", AgentID: "a1", Status: "running",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sess.Status = "completed"
	if err := store.UpdateSession(ctx, sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}

	got, err := store.GetSession(ctx, "su1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("Status = %q, want completed", got.Status)
	}
}

func TestListSessionsPagination(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		sess := &storage.Session{
			ID: fmt.Sprintf("sp%d", i), AgentID: "a1", Status: "completed",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := store.CreateSession(ctx, sess); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}

	page1, err := store.ListSessions(ctx, "a1", 2, 0)
	if err != nil {
		t.Fatalf("ListSessions page1: %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("page1 len = %d, want 2", len(page1))
	}

	page2, err := store.ListSessions(ctx, "a1", 2, 2)
	if err != nil {
		t.Fatalf("ListSessions page2: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("page2 len = %d, want 2", len(page2))
	}
}

func TestMemoryUpsert(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mem := &storage.MemoryRecord{
		ID: "mu1", AgentID: "a1", Kind: "long_term",
		Key: "preference", Value: "dark",
	}
	if err := store.PutMemory(ctx, mem); err != nil {
		t.Fatalf("PutMemory: %v", err)
	}

	mem.Value = "light"
	if err := store.PutMemory(ctx, mem); err != nil {
		t.Fatalf("PutMemory (upsert): %v", err)
	}

	got, err := store.GetMemory(ctx, "a1", "preference")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.Value != "light" {
		t.Errorf("Value = %v, want light", got.Value)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.GetSession(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestGetCheckpoint_NotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.GetCheckpoint(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent checkpoint")
	}
}

func TestGetLatestCheckpoint_NoCheckpoints(t *testing.T) {
	store := newTestStore(t)
	_, err := store.GetLatestCheckpoint(context.Background(), "no-session")
	if err == nil {
		t.Error("expected error when no checkpoints exist")
	}
}

func TestMultipleCheckpoints_LatestReturnsNewest(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t1 := time.Now()
	for i := 0; i < 3; i++ {
		cp := &storage.Checkpoint{
			ID: fmt.Sprintf("cp%d", i), SessionID: "s1", RunID: "r1",
			NodeID: fmt.Sprintf("n%d", i), SeqNum: int64(i + 1),
			State: map[string]any{"step": i}, CreatedAt: t1.Add(time.Duration(i) * time.Second),
		}
		if err := store.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatalf("SaveCheckpoint: %v", err)
		}
	}

	latest, err := store.GetLatestCheckpoint(ctx, "s1")
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if latest.ID != "cp2" {
		t.Errorf("latest ID = %q, want cp2", latest.ID)
	}

	all, err := store.ListCheckpoints(ctx, "s1")
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 checkpoints, got %d", len(all))
	}
}

func TestMigrateIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate should be idempotent: %v", err)
	}
}
