package sqlite

import (
	"context"
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

	if err := store.DeleteMemory(ctx, "m1"); err != nil {
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
