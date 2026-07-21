package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/spawn08/chronos/storage"
)

// newTestStore spins up an in-process miniredis and returns a Store bound to it.
func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	store := NewWithClient(client)
	t.Cleanup(func() { _ = store.Close() })
	return store, mr
}

func TestNew_ConnectRefused(t *testing.T) {
	// Port 1 is reserved and will refuse the TCP connection.
	if _, err := New("127.0.0.1:1", "", 0); err == nil {
		t.Fatal("expected connection error, got nil")
	}
}

func TestNew_PingSucceeds(t *testing.T) {
	mr := miniredis.RunT(t)
	s, err := New(mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestKeyFunctions(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"sessionKey", sessionKey("s1"), "chronos:session:s1"},
		{"memoryKey", memoryKey("m1"), "chronos:memory:m1"},
		{"auditKey", auditKey("a1"), "chronos:audit:a1"},
		{"traceKey", traceKey("t1"), "chronos:trace:t1"},
		{"eventKey", eventKey("e1"), "chronos:event:e1"},
		{"checkpointKey", checkpointKey("cp1"), "chronos:checkpoint:cp1"},
		{"sessionIndexKey", sessionIndexKey("a1"), "chronos:idx:sessions:a1"},
		{"auditIndexKey", auditIndexKey("s1"), "chronos:idx:audits:s1"},
		{"traceIndexKey", traceIndexKey("s1"), "chronos:idx:traces:s1"},
		{"eventIndexKey", eventIndexKey("s1"), "chronos:idx:events:s1"},
		{"checkpointIndexKey", checkpointIndexKey("s1"), "chronos:idx:checkpoints:s1"},
		{"memoryIndexKey", memoryIndexKey("a1", "long_term"), "chronos:idx:memory:a1:long_term"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestSessionCRUD(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	sess := &storage.Session{ID: "s1", AgentID: "agent-1", Status: "running", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := store.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != "s1" || got.AgentID != "agent-1" {
		t.Errorf("got %+v", got)
	}

	sess.Status = "completed"
	if err := store.UpdateSession(ctx, sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	got2, _ := store.GetSession(ctx, "s1")
	if got2.Status != "completed" {
		t.Errorf("Status = %q, want completed", got2.Status)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	if _, err := store.GetSession(context.Background(), "missing"); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestListSessions_OrderAndPagination(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	base := time.Now()

	for i := 0; i < 3; i++ {
		s := &storage.Session{
			ID:        string(rune('a' + i)),
			AgentID:   "agent-1",
			Status:    "running",
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if err := store.CreateSession(ctx, s); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}

	all, err := store.ListSessions(ctx, "agent-1", 10, 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(all))
	}
	// Newest first (highest CreatedAt score).
	if all[0].ID != "c" {
		t.Errorf("first session = %q, want c (newest)", all[0].ID)
	}

	page, err := store.ListSessions(ctx, "agent-1", 1, 1)
	if err != nil {
		t.Fatalf("ListSessions page: %v", err)
	}
	if len(page) != 1 || page[0].ID != "b" {
		t.Errorf("page = %+v, want single session b", page)
	}
}

func TestListSessions_DefaultLimit(t *testing.T) {
	store, _ := newTestStore(t)
	got, err := store.ListSessions(context.Background(), "none", 0, 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil slice")
	}
}

func TestMemoryCRUD(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// GetMemory derives id as mem_<agent>_lt_<key>; store under that id.
	id := "mem_agent-1_lt_fact"
	mem := &storage.MemoryRecord{ID: id, AgentID: "agent-1", Kind: "long_term", Key: "fact", Value: "Alice", CreatedAt: time.Now()}
	if err := store.PutMemory(ctx, mem); err != nil {
		t.Fatalf("PutMemory: %v", err)
	}

	got, err := store.GetMemory(ctx, "agent-1", "fact")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.Value != "Alice" {
		t.Errorf("Value = %v, want Alice", got.Value)
	}

	list, err := store.ListMemory(ctx, "agent-1", "long_term")
	if err != nil {
		t.Fatalf("ListMemory: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 record, got %d", len(list))
	}

	if err := store.DeleteMemory(ctx, id); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	if _, err := store.GetMemory(ctx, "agent-1", "fact"); err == nil {
		t.Error("expected not-found after delete")
	}
}

func TestAuditLogs(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	log := &storage.AuditLog{ID: "a1", SessionID: "sess-1", Actor: "user", Action: "chat", Resource: "agent", CreatedAt: time.Now()}
	if err := store.AppendAuditLog(ctx, log); err != nil {
		t.Fatalf("AppendAuditLog: %v", err)
	}
	logs, err := store.ListAuditLogs(ctx, "sess-1", 10, 0)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 audit log, got %d", len(logs))
	}
}

func TestTraceCRUD(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	tr := &storage.Trace{ID: "t1", SessionID: "sess-1", Name: "chat", Kind: "agent", StartedAt: time.Now()}
	if err := store.InsertTrace(ctx, tr); err != nil {
		t.Fatalf("InsertTrace: %v", err)
	}
	got, err := store.GetTrace(ctx, "t1")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if got.ID != "t1" {
		t.Errorf("ID = %q, want t1", got.ID)
	}
	if _, err = store.GetTrace(ctx, "missing"); err == nil {
		t.Error("expected not-found for missing trace")
	}
	traces, err := store.ListTraces(ctx, "sess-1")
	if err != nil {
		t.Fatalf("ListTraces: %v", err)
	}
	if len(traces) != 1 {
		t.Errorf("expected 1 trace, got %d", len(traces))
	}
}

func TestEvents_OrderAndFilter(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		e := &storage.Event{ID: string(rune('a' + i)), SessionID: "sess-1", SeqNum: int64(i), Type: "t", Payload: map[string]any{"n": i}}
		if err := store.AppendEvent(ctx, e); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	all, err := store.ListEvents(ctx, "sess-1", 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 events, got %d", len(all))
	}
	if all[0].SeqNum != 1 || all[2].SeqNum != 3 {
		t.Errorf("events not sorted ascending by seq: %+v", all)
	}

	after, _ := store.ListEvents(ctx, "sess-1", 1)
	if len(after) != 2 {
		t.Errorf("expected 2 events after seq=1, got %d", len(after))
	}
}

func TestCheckpointCRUD(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		cp := &storage.Checkpoint{ID: string(rune('a' + i)), SessionID: "sess-1", RunID: "r1", NodeID: "n1", State: map[string]any{"k": i}, SeqNum: int64(i), CreatedAt: time.Now()}
		if err := store.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatalf("SaveCheckpoint: %v", err)
		}
	}

	got, err := store.GetCheckpoint(ctx, "b")
	if err != nil {
		t.Fatalf("GetCheckpoint: %v", err)
	}
	if got.ID != "b" {
		t.Errorf("ID = %q, want b", got.ID)
	}

	list, err := store.ListCheckpoints(ctx, "sess-1")
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 checkpoints, got %d", len(list))
	}

	latest, err := store.GetLatestCheckpoint(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if latest.SeqNum != 2 {
		t.Errorf("latest SeqNum = %d, want 2", latest.SeqNum)
	}
}

func TestGetLatestCheckpoint_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	if _, err := store.GetLatestCheckpoint(context.Background(), "missing"); err == nil {
		t.Fatal("expected error for missing checkpoint")
	}
}

func TestMigrateAndCloseNil(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.Migrate(context.Background()); err != nil {
		t.Errorf("Migrate: %v", err)
	}
	var empty Store
	if err := empty.Close(); err != nil {
		t.Errorf("Close on zero-value Store: %v", err)
	}
}

func TestMarshalError(t *testing.T) {
	store, _ := newTestStore(t)
	// channels cannot be JSON-marshaled.
	if err := store.set(context.Background(), "k", make(chan int)); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestCompileTimeInterface(t *testing.T) {
	var _ storage.Storage = (*Store)(nil)
}
