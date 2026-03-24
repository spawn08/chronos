package graph

import (
	"context"
	"fmt"
	"testing"

	"github.com/spawn08/chronos/storage"
)

// TestRunner_Resume_NotFound tests that Resume fails gracefully when no checkpoint exists.
func TestRunner_Resume_NotFound(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("node_a")
	runner := NewRunner(compiled, store)

	_, err := runner.Resume(context.Background(), "nonexistent-session")
	if err == nil {
		t.Fatal("expected error for nonexistent session checkpoint")
	}
}

// TestRunner_Resume_Success tests Resume with an existing checkpoint.
func TestRunner_Resume_Success(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("node_a", "node_b")

	// Run to create checkpoints
	runner1 := NewRunner(compiled, store)
	result1, err := runner1.Run(context.Background(), "sess-resume-success", State{"val": "hello"})
	if err != nil {
		t.Fatalf("initial Run: %v", err)
	}
	if result1 == nil {
		t.Fatal("expected result")
	}

	// Resume from latest checkpoint
	runner2 := NewRunner(compiled, store)
	result2, err := runner2.Resume(context.Background(), "sess-resume-success")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result2 == nil {
		t.Fatal("expected non-nil resume result")
	}
}

// TestRunner_ResumeFromCheckpoint verifies that ResumeFromCheckpoint resumes from a saved checkpoint.
func TestRunner_ResumeFromCheckpoint(t *testing.T) {
	store := newRunnerTestStorage()

	// Build a simple 2-node graph
	compiled := buildLinearGraph("node_a", "node_b")

	// Run to completion so checkpoints are saved
	runner1 := NewRunner(compiled, store)
	_, err := runner1.Run(context.Background(), "sess-resume", State{"data": "hello"})
	if err != nil {
		t.Fatalf("initial Run: %v", err)
	}

	// Get a checkpoint
	store.mu.Lock()
	cps := make([]*storage.Checkpoint, len(store.checkpoints))
	copy(cps, store.checkpoints)
	store.mu.Unlock()

	if len(cps) == 0 {
		t.Fatal("expected at least one checkpoint")
	}

	// Resume from the first checkpoint using a fresh runner
	cp := cps[0]
	runner2 := NewRunner(compiled, store)
	result, err := runner2.ResumeFromCheckpoint(context.Background(), cp.ID)
	if err != nil {
		t.Fatalf("ResumeFromCheckpoint: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestRunner_ResumeFromCheckpoint_NotFound tests that a non-existent checkpoint returns an error.
func TestRunner_ResumeFromCheckpoint_NotFound(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("node_a")
	runner := NewRunner(compiled, store)

	_, err := runner.ResumeFromCheckpoint(context.Background(), "nonexistent-cp-id")
	if err == nil {
		t.Fatal("expected error for nonexistent checkpoint")
	}
}

// TestRunner_ForkFrom verifies that ForkFrom creates a new branch from a checkpoint.
func TestRunner_ForkFrom(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("node_a", "node_b")

	// Run to get checkpoints
	runner1 := NewRunner(compiled, store)
	_, err := runner1.Run(context.Background(), "sess-fork", State{"val": "original"})
	if err != nil {
		t.Fatalf("initial Run: %v", err)
	}

	store.mu.Lock()
	cps := make([]*storage.Checkpoint, len(store.checkpoints))
	copy(cps, store.checkpoints)
	store.mu.Unlock()

	if len(cps) == 0 {
		t.Fatal("expected at least one checkpoint")
	}

	// Fork from the first checkpoint with a state update using a fresh runner
	cp := cps[0]
	runner2 := NewRunner(compiled, store)
	result, err := runner2.ForkFrom(context.Background(), cp.ID, map[string]any{
		"val": "forked",
	})
	if err != nil {
		t.Fatalf("ForkFrom: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil fork result")
	}
}

// TestRunner_ForkFrom_NotFound tests error handling for non-existent checkpoint.
func TestRunner_ForkFrom_NotFound(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("node_a")
	runner := NewRunner(compiled, store)

	_, err := runner.ForkFrom(context.Background(), "nonexistent-cp", map[string]any{"x": 1})
	if err == nil {
		t.Fatal("expected error for nonexistent checkpoint")
	}
}

// TestRunner_ReplayFrom verifies that ReplayFrom re-executes from a checkpoint.
func TestRunner_ReplayFrom(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("node_a", "node_b")

	// Run to get checkpoints
	runner1 := NewRunner(compiled, store)
	_, err := runner1.Run(context.Background(), "sess-replay", State{"step": 0})
	if err != nil {
		t.Fatalf("initial Run: %v", err)
	}

	store.mu.Lock()
	cps := make([]*storage.Checkpoint, len(store.checkpoints))
	copy(cps, store.checkpoints)
	store.mu.Unlock()

	if len(cps) == 0 {
		t.Fatal("expected at least one checkpoint")
	}

	// Replay from the first checkpoint using a fresh runner
	cp := cps[0]
	runner2 := NewRunner(compiled, store)
	result, err := runner2.ReplayFrom(context.Background(), cp.ID)
	if err != nil {
		t.Fatalf("ReplayFrom: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil replay result")
	}
}

// TestRunner_ReplayFrom_NotFound tests error handling.
func TestRunner_ReplayFrom_NotFound(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("node_a")
	runner := NewRunner(compiled, store)

	_, err := runner.ReplayFrom(context.Background(), "nonexistent-checkpoint")
	if err == nil {
		t.Fatal("expected error for nonexistent checkpoint")
	}
}

// TestRunner_ForkFrom_SessionCreationError verifies ForkFrom fails gracefully if session creation fails.
func TestRunner_ForkFrom_SessionCreationError(t *testing.T) {
	normalStore := newRunnerTestStorage()
	compiled := buildLinearGraph("node_a", "node_b")

	// First run with a normal store to get a checkpoint
	runner1 := NewRunner(compiled, normalStore)
	_, err := runner1.Run(context.Background(), "sess-orig", State{})
	if err != nil {
		t.Fatalf("initial run: %v", err)
	}

	normalStore.mu.Lock()
	cps := normalStore.checkpoints
	normalStore.mu.Unlock()

	if len(cps) == 0 {
		t.Skip("no checkpoints")
	}

	// Create a store that fails on CreateSession but has the checkpoints
	failStore := &failSessionStore{inner: normalStore}
	runner2 := NewRunner(compiled, failStore)
	_, err = runner2.ForkFrom(context.Background(), cps[0].ID, map[string]any{})
	if err == nil {
		t.Fatal("expected error when session creation fails")
	}
}

// failSessionStore wraps a storage and fails on CreateSession.
type failSessionStore struct {
	inner *runnerTestStorage
}

func (s *failSessionStore) CreateSession(_ context.Context, _ *storage.Session) error {
	return fmt.Errorf("session creation failed intentionally")
}

func (s *failSessionStore) GetSession(ctx context.Context, id string) (*storage.Session, error) {
	return s.inner.GetSession(ctx, id)
}

func (s *failSessionStore) UpdateSession(ctx context.Context, sess *storage.Session) error {
	return s.inner.UpdateSession(ctx, sess)
}

func (s *failSessionStore) ListSessions(ctx context.Context, id string, a, b int) ([]*storage.Session, error) {
	return s.inner.ListSessions(ctx, id, a, b)
}

func (s *failSessionStore) AppendEvent(ctx context.Context, e *storage.Event) error {
	return s.inner.AppendEvent(ctx, e)
}

func (s *failSessionStore) ListEvents(ctx context.Context, id string, seq int64) ([]*storage.Event, error) {
	return s.inner.ListEvents(ctx, id, seq)
}

func (s *failSessionStore) SaveCheckpoint(ctx context.Context, cp *storage.Checkpoint) error {
	return s.inner.SaveCheckpoint(ctx, cp)
}

func (s *failSessionStore) GetCheckpoint(ctx context.Context, id string) (*storage.Checkpoint, error) {
	return s.inner.GetCheckpoint(ctx, id)
}

func (s *failSessionStore) GetLatestCheckpoint(ctx context.Context, id string) (*storage.Checkpoint, error) {
	return s.inner.GetLatestCheckpoint(ctx, id)
}

func (s *failSessionStore) ListCheckpoints(ctx context.Context, id string) ([]*storage.Checkpoint, error) {
	return s.inner.ListCheckpoints(ctx, id)
}

func (s *failSessionStore) InsertTrace(ctx context.Context, t *storage.Trace) error {
	return s.inner.InsertTrace(ctx, t)
}

func (s *failSessionStore) GetTrace(ctx context.Context, id string) (*storage.Trace, error) {
	return s.inner.GetTrace(ctx, id)
}

func (s *failSessionStore) ListTraces(ctx context.Context, id string) ([]*storage.Trace, error) {
	return s.inner.ListTraces(ctx, id)
}

func (s *failSessionStore) AppendAuditLog(ctx context.Context, l *storage.AuditLog) error {
	return s.inner.AppendAuditLog(ctx, l)
}

func (s *failSessionStore) ListAuditLogs(ctx context.Context, id string, a, b int) ([]*storage.AuditLog, error) {
	return s.inner.ListAuditLogs(ctx, id, a, b)
}

func (s *failSessionStore) PutMemory(ctx context.Context, m *storage.MemoryRecord) error {
	return s.inner.PutMemory(ctx, m)
}

func (s *failSessionStore) GetMemory(ctx context.Context, a, b string) (*storage.MemoryRecord, error) {
	return s.inner.GetMemory(ctx, a, b)
}

func (s *failSessionStore) ListMemory(ctx context.Context, a, b string) ([]*storage.MemoryRecord, error) {
	return s.inner.ListMemory(ctx, a, b)
}

func (s *failSessionStore) DeleteMemory(ctx context.Context, id string) error {
	return s.inner.DeleteMemory(ctx, id)
}

func (s *failSessionStore) Migrate(ctx context.Context) error { return nil }
func (s *failSessionStore) Close() error                      { return nil }
