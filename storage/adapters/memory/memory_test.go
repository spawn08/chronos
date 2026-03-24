package memory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := New()
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

func TestSessionCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sess := &storage.Session{
		ID:        "s1",
		AgentID:   "agent1",
		Status:    "running",
		Metadata:  map[string]any{"foo": "bar"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}

	if err := s.CreateSession(ctx, sess); err == nil {
		t.Error("expected duplicate session error")
	}

	got, err := s.GetSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentID != "agent1" {
		t.Errorf("got agent_id=%q, want agent1", got.AgentID)
	}

	_, err = s.GetSession(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}

	sess.Status = "completed"
	if err := s.UpdateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSession(ctx, "s1")
	if got.Status != "completed" {
		t.Errorf("got status=%q, want completed", got.Status)
	}

	if err := s.UpdateSession(ctx, &storage.Session{ID: "nonexistent"}); err == nil {
		t.Error("expected error updating nonexistent session")
	}

	list, err := s.ListSessions(ctx, "agent1", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("got %d sessions, want 1", len(list))
	}

	list, _ = s.ListSessions(ctx, "other", 10, 0)
	if len(list) != 0 {
		t.Errorf("got %d sessions for other agent, want 0", len(list))
	}
}

func TestMemoryCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	m := &storage.MemoryRecord{
		ID:        "m1",
		AgentID:   "agent1",
		Kind:      "short_term",
		Key:       "greeting",
		Value:     "hello",
		CreatedAt: time.Now(),
	}
	if err := s.PutMemory(ctx, m); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetMemory(ctx, "agent1", "greeting")
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != "greeting" {
		t.Errorf("got key=%q, want greeting", got.Key)
	}

	_, err = s.GetMemory(ctx, "agent1", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent memory")
	}

	list, err := s.ListMemory(ctx, "agent1", "short_term")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("got %d memory records, want 1", len(list))
	}

	if err := s.DeleteMemory(ctx, "m1"); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListMemory(ctx, "agent1", "short_term")
	if len(list) != 0 {
		t.Errorf("got %d after delete, want 0", len(list))
	}
}

func TestAuditLogCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	log := &storage.AuditLog{
		ID:        "al1",
		SessionID: "s1",
		Actor:     "user1",
		Action:    "create",
		Resource:  "session",
		CreatedAt: time.Now(),
	}
	if err := s.AppendAuditLog(ctx, log); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListAuditLogs(ctx, "s1", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("got %d audit logs, want 1", len(list))
	}

	list, _ = s.ListAuditLogs(ctx, "s1", 10, 100)
	if len(list) != 0 {
		t.Errorf("got %d audit logs with high offset, want 0", len(list))
	}
}

func TestTraceCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tr := &storage.Trace{
		ID:        "t1",
		SessionID: "s1",
		Name:      "test",
		Kind:      "node",
		StartedAt: time.Now(),
	}
	if err := s.InsertTrace(ctx, tr); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTrace(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "test" {
		t.Errorf("got name=%q, want test", got.Name)
	}

	_, err = s.GetTrace(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent trace")
	}

	list, err := s.ListTraces(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("got %d traces, want 1", len(list))
	}
}

func TestEventCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	e := &storage.Event{
		ID:        "e1",
		SessionID: "s1",
		SeqNum:    1,
		Type:      "node_executed",
		CreatedAt: time.Now(),
	}
	if err := s.AppendEvent(ctx, e); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListEvents(ctx, "s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("got %d events, want 1", len(list))
	}

	list, _ = s.ListEvents(ctx, "s1", 1)
	if len(list) != 0 {
		t.Errorf("got %d events after seq 1, want 0", len(list))
	}
}

func TestCheckpointCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cp1 := &storage.Checkpoint{
		ID:        "cp1",
		SessionID: "s1",
		RunID:     "r1",
		NodeID:    "node_a",
		State:     map[string]any{"step": 1},
		SeqNum:    1,
		CreatedAt: time.Now(),
	}
	cp2 := &storage.Checkpoint{
		ID:        "cp2",
		SessionID: "s1",
		RunID:     "r1",
		NodeID:    "node_b",
		State:     map[string]any{"step": 2},
		SeqNum:    2,
		CreatedAt: time.Now().Add(time.Second),
	}
	if err := s.SaveCheckpoint(ctx, cp1); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCheckpoint(ctx, cp2); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetCheckpoint(ctx, "cp1")
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeID != "node_a" {
		t.Errorf("got node_id=%q, want node_a", got.NodeID)
	}

	_, err = s.GetCheckpoint(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent checkpoint")
	}

	latest, err := s.GetLatestCheckpoint(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != "cp2" {
		t.Errorf("got latest=%q, want cp2", latest.ID)
	}

	_, err = s.GetLatestCheckpoint(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session checkpoints")
	}

	list, err := s.ListCheckpoints(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("got %d checkpoints, want 2", len(list))
	}
	if list[0].SeqNum > list[1].SeqNum {
		t.Error("checkpoints not sorted by seq_num")
	}
}

func TestClose(t *testing.T) {
	s := New()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestListSessionsPagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = s.CreateSession(ctx, &storage.Session{
			ID:        fmt.Sprintf("s%d", i),
			AgentID:   "agent1",
			Status:    "running",
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
			UpdatedAt: time.Now(),
		})
	}

	list, _ := s.ListSessions(ctx, "agent1", 2, 0)
	if len(list) != 2 {
		t.Errorf("got %d, want 2", len(list))
	}

	list, _ = s.ListSessions(ctx, "agent1", 2, 4)
	if len(list) != 1 {
		t.Errorf("got %d, want 1", len(list))
	}

	list, _ = s.ListSessions(ctx, "agent1", 10, 10)
	if len(list) != 0 {
		t.Errorf("got %d with large offset, want 0", len(list))
	}
}
