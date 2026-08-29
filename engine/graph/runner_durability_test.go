package graph

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

// buildApprovalGraph builds A -> pause(interrupt) -> B -> END, incrementing the
// supplied counters each time a node runs so tests can assert exactly-once
// execution across a pause/resume.
func buildApprovalGraph(a, pause, b *int64) *CompiledGraph {
	g := New("approval")
	g.AddNode("A", func(_ context.Context, s State) (State, error) {
		atomic.AddInt64(a, 1)
		return s, nil
	})
	g.AddInterruptNode("pause", func(_ context.Context, s State) (State, error) {
		atomic.AddInt64(pause, 1)
		s["approved"] = true
		return s, nil
	})
	g.AddNode("B", func(_ context.Context, s State) (State, error) {
		atomic.AddInt64(b, 1)
		return s, nil
	})
	g.SetEntryPoint("A")
	g.AddEdge("A", "pause")
	g.AddEdge("pause", "B")
	g.SetFinishPoint("B")
	compiled, _ := g.Compile()
	return compiled
}

// waitClosed fails the test if ch is not closed within timeout.
func waitClosed(t *testing.T, ch <-chan StreamEvent, timeout time.Duration) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timer.C:
			t.Fatal("stream channel was not closed within timeout")
		}
	}
}

// Resuming a paused approval workflow advances PAST the interrupt node exactly
// once and never re-runs the already-completed node.
func TestRunner_ResumePastInterrupt_NoDoubleExecution(t *testing.T) {
	ctx := context.Background()
	var aCount, pauseCount, bCount int64
	compiled := buildApprovalGraph(&aCount, &pauseCount, &bCount)
	store := newRunnerTestStorage()

	// First run pauses at the interrupt node, after A has executed once.
	r1 := NewRunner(compiled, store)
	rs1, err := r1.Run(ctx, "sess", State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rs1.Status != RunStatusPaused {
		t.Fatalf("status = %q, want paused", rs1.Status)
	}
	if got := atomic.LoadInt64(&aCount); got != 1 {
		t.Fatalf("A executed %d times before pause, want 1", got)
	}
	if got := atomic.LoadInt64(&pauseCount); got != 0 {
		t.Fatalf("pause node executed %d times before resume, want 0", got)
	}

	// The latest checkpoint records the NEXT node to run (the interrupt node),
	// not the just-completed node A.
	cp, err := store.GetLatestCheckpoint(ctx, "sess")
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if cp.NodeID != "pause" {
		t.Errorf("latest checkpoint NodeID = %q, want pause", cp.NodeID)
	}

	// Resume with a fresh runner: advances past the interrupt exactly once.
	r2 := NewRunner(compiled, store)
	rs2, err := r2.Resume(ctx, "sess")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if rs2.Status != RunStatusCompleted {
		t.Errorf("resumed status = %q, want completed", rs2.Status)
	}
	if got := atomic.LoadInt64(&aCount); got != 1 {
		t.Errorf("A executed %d times total, want 1 (no double execution)", got)
	}
	if got := atomic.LoadInt64(&pauseCount); got != 1 {
		t.Errorf("pause node executed %d times, want 1 (advance past once)", got)
	}
	if got := atomic.LoadInt64(&bCount); got != 1 {
		t.Errorf("B executed %d times, want 1", got)
	}
}

// A plain (non-interrupt) resume continues from the next node and does not
// re-run the node captured by the checkpoint.
func TestRunner_ResumeFromCheckpoint_NoReExecution(t *testing.T) {
	ctx := context.Background()
	var counts sync.Map // node -> *int64
	inc := func(node string) {
		v, _ := counts.LoadOrStore(node, new(int64))
		atomic.AddInt64(v.(*int64), 1)
	}
	get := func(node string) int64 {
		v, ok := counts.Load(node)
		if !ok {
			return 0
		}
		return atomic.LoadInt64(v.(*int64))
	}

	g := New("linear")
	for _, n := range []string{"n1", "n2", "n3"} {
		node := n
		g.AddNode(node, func(_ context.Context, s State) (State, error) {
			inc(node)
			return s, nil
		})
	}
	g.SetEntryPoint("n1")
	g.AddEdge("n1", "n2")
	g.AddEdge("n2", "n3")
	g.SetFinishPoint("n3")
	compiled, _ := g.Compile()

	store := newRunnerTestStorage()
	r1 := NewRunner(compiled, store)
	if _, err := r1.Run(ctx, "s", State{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if get("n1") != 1 || get("n2") != 1 || get("n3") != 1 {
		t.Fatalf("initial run counts n1=%d n2=%d n3=%d, want all 1", get("n1"), get("n2"), get("n3"))
	}

	// Grab the checkpoint written after n1 executed (its NodeID should be n2).
	store.mu.Lock()
	var afterN1 *storage.Checkpoint
	for _, cp := range store.checkpoints {
		if cp.NodeID == "n2" {
			afterN1 = cp
			break
		}
	}
	store.mu.Unlock()
	if afterN1 == nil {
		t.Fatal("expected a checkpoint pointing at n2 (next node after n1)")
		return
	}

	// Resume from that checkpoint: n1 must NOT run again; n2 and n3 run once more.
	r2 := NewRunner(compiled, store)
	rs, err := r2.ResumeFromCheckpoint(ctx, afterN1.ID)
	if err != nil {
		t.Fatalf("ResumeFromCheckpoint: %v", err)
	}
	if rs.Status != RunStatusCompleted {
		t.Errorf("status = %q, want completed", rs.Status)
	}
	if get("n1") != 1 {
		t.Errorf("n1 executed %d times, want 1 (resume must not re-run it)", get("n1"))
	}
	if get("n2") != 2 || get("n3") != 2 {
		t.Errorf("n2=%d n3=%d, want 2 each after resume", get("n2"), get("n3"))
	}
}

// Regression: resuming from a NON-interrupt checkpoint that lies upstream
// of an interrupt must still pause at that interrupt. The skip-once behavior
// applies only to the resumed node, never to a downstream gate. This models
// crash recovery / replay landing on a normal node with an unapproved
// human-in-the-loop gate ahead — the previous flag misfired and ran the gate.
func TestRunner_ResumeFromNormalNode_StillPausesAtDownstreamInterrupt(t *testing.T) {
	ctx := context.Background()
	var n1Count, gateCount, n3Count int64

	// n1(normal) -> gate(interrupt) -> n3.
	g := New("downstream-gate")
	g.AddNode("n1", func(_ context.Context, s State) (State, error) {
		atomic.AddInt64(&n1Count, 1)
		return s, nil
	})
	g.AddInterruptNode("gate", func(_ context.Context, s State) (State, error) {
		atomic.AddInt64(&gateCount, 1)
		return s, nil
	})
	g.AddNode("n3", func(_ context.Context, s State) (State, error) {
		atomic.AddInt64(&n3Count, 1)
		return s, nil
	})
	g.SetEntryPoint("n1")
	g.AddEdge("n1", "gate")
	g.AddEdge("gate", "n3")
	g.SetFinishPoint("n3")
	compiled, _ := g.Compile()

	store := newRunnerTestStorage()

	// Seed a checkpoint whose node is the NORMAL n1 (the resume point), as a
	// crash mid-run or a ReplayFrom would land on.
	seed := &storage.Checkpoint{
		ID: "cp_sess_0", SessionID: "sess", RunID: "r", NodeID: "n1",
		State: map[string]any{}, SeqNum: 0, CreatedAt: time.Now(),
	}
	if err := store.SaveCheckpoint(ctx, seed); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	// Resume from the normal-node checkpoint (skipFirstInterrupt=true internally).
	rs, err := NewRunner(compiled, store).ResumeFromCheckpoint(ctx, seed.ID)
	if err != nil {
		t.Fatalf("ResumeFromCheckpoint: %v", err)
	}

	if rs.Status != RunStatusPaused {
		t.Fatalf("resumed status = %q, want paused (downstream gate must not be skipped)", rs.Status)
	}
	if got := atomic.LoadInt64(&n1Count); got != 1 {
		t.Errorf("n1 executed %d times, want 1 (resumed node runs)", got)
	}
	if got := atomic.LoadInt64(&gateCount); got != 0 {
		t.Errorf("gate node executed %d times, want 0 (must pause for approval)", got)
	}
	if got := atomic.LoadInt64(&n3Count); got != 0 {
		t.Errorf("n3 executed %d times, want 0 (run must not proceed past the gate)", got)
	}
}

// The stream channel is closed on the pause exit path.
func TestRunner_ChannelClosedOnPause(t *testing.T) {
	var a, p, b int64
	compiled := buildApprovalGraph(&a, &p, &b)
	runner := NewRunner(compiled, newRunnerTestStorage())
	ch := runner.Stream()
	if _, err := runner.Run(context.Background(), "s", State{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	waitClosed(t, ch, time.Second)
}

// The stream channel is closed on the error exit path.
func TestRunner_ChannelClosedOnError(t *testing.T) {
	g := New("boom")
	g.AddNode("x", func(_ context.Context, _ State) (State, error) {
		return nil, errors.New("boom")
	})
	g.SetEntryPoint("x")
	g.SetFinishPoint("x")
	compiled, _ := g.Compile()

	runner := NewRunner(compiled, newRunnerTestStorage())
	ch := runner.Stream()
	if _, err := runner.Run(context.Background(), "s", State{}); err == nil {
		t.Fatal("expected error")
	}
	waitClosed(t, ch, time.Second)
}

// Reusing a Runner returns an error rather than panicking on a closed channel.
func TestRunner_ReuseReturnsError(t *testing.T) {
	runner := NewRunner(buildLinearGraph("a"), newRunnerTestStorage())
	if _, err := runner.Run(context.Background(), "s1", State{"visited": ""}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	_, err := runner.Run(context.Background(), "s2", State{"visited": ""})
	if err == nil {
		t.Fatal("expected error on Runner reuse, got nil")
		return
	}
	if !strings.Contains(err.Error(), "already used") {
		t.Errorf("error = %q, want it to mention reuse", err)
	}
}

// Concurrent emits + broker fan-out must not panic or race.
func TestRunner_ConcurrentEmitNoRace(t *testing.T) {
	names := make([]string, 30)
	for i := range names {
		names[i] = "n" + strconv.Itoa(i)
	}
	compiled := buildLinearGraph(names...)

	broker := stream.NewBroker()
	sub := broker.Subscribe("race-sub")
	defer broker.Unsubscribe("race-sub")

	runner := NewRunner(compiled, newRunnerTestStorage()).WithBroker(broker)
	ch := runner.Stream()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range sub {
		}
	}()

	if _, err := runner.Run(context.Background(), "s", State{"visited": ""}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	waitClosed(t, ch, 2*time.Second)
	broker.Unsubscribe("race-sub")
	<-done
}

// A cyclic graph terminates with a clear error instead of looping.
func TestRunner_MaxStepsGuard(t *testing.T) {
	g := New("cyclic")
	g.AddNode("loop", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("loop")
	g.AddConditionalEdge("loop", func(_ State) string { return "loop" })
	compiled, _ := g.Compile()

	runner := NewRunner(compiled, newRunnerTestStorage()).WithMaxSteps(10)
	rs, err := runner.Run(context.Background(), "s", State{})
	if err == nil {
		t.Fatal("expected max-steps error for cyclic graph")
		return
	}
	if !strings.Contains(err.Error(), "max steps") {
		t.Errorf("error = %q, want it to mention max steps", err)
	}
	if rs.Status != RunStatusFailed {
		t.Errorf("status = %q, want failed", rs.Status)
	}
}

// A node that runs longer than the per-node timeout fails the run.
func TestRunner_NodeTimeout(t *testing.T) {
	g := New("slow")
	g.AddNode("slow", func(ctx context.Context, s State) (State, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
			return s, nil
		}
	})
	g.SetEntryPoint("slow")
	g.SetFinishPoint("slow")
	compiled, _ := g.Compile()

	runner := NewRunner(compiled, newRunnerTestStorage()).WithNodeTimeout(20 * time.Millisecond)
	rs, err := runner.Run(context.Background(), "s", State{})
	if err == nil {
		t.Fatal("expected node timeout error")
		return
	}
	if rs.Status != RunStatusFailed {
		t.Errorf("status = %q, want failed", rs.Status)
	}
}

// A panicking node is converted to an error; the run fails cleanly and emits an
// error event instead of crashing the process.
func TestRunner_PanicRecovery(t *testing.T) {
	g := New("panic")
	g.AddNode("kaboom", func(_ context.Context, _ State) (State, error) {
		panic("node blew up")
	})
	g.SetEntryPoint("kaboom")
	g.SetFinishPoint("kaboom")
	compiled, _ := g.Compile()

	runner := NewRunner(compiled, newRunnerTestStorage())
	ch := runner.Stream()

	rs, err := runner.Run(context.Background(), "s", State{})
	if err == nil {
		t.Fatal("expected error from panicking node")
		return
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Errorf("error = %q, want it to mention the panic", err)
	}
	if rs.Status != RunStatusFailed {
		t.Errorf("status = %q, want failed", rs.Status)
	}

	events := drainChannel(ch, 500*time.Millisecond)
	foundErr := false
	for _, e := range events {
		if e.Type == "error" {
			foundErr = true
		}
	}
	if !foundErr {
		t.Error("expected an error event on the stream")
	}
}

// --- append error surfaced; committer used when present ---

// failEventStore fails AppendEvent so the runner must surface (not discard) it.
type failEventStore struct {
	*runnerTestStorage
}

func (s *failEventStore) AppendEvent(_ context.Context, _ *storage.Event) error {
	return errors.New("append failed")
}

// The runner must not discard the AppendEvent error.
func TestRunner_AppendEventErrorSurfaced(t *testing.T) {
	store := &failEventStore{runnerTestStorage: newRunnerTestStorage()}
	runner := NewRunner(buildLinearGraph("a", "b"), store)
	_, err := runner.Run(context.Background(), "s", State{"visited": ""})
	if err == nil {
		t.Fatal("expected the run to fail when AppendEvent errors")
		return
	}
}

// committerStore implements graph.CheckpointCommitter and records its use.
type committerStore struct {
	*runnerTestStorage
	commitCalls  int64
	appendCalled int64
}

func (s *committerStore) SaveCheckpointAndEvent(ctx context.Context, cp *storage.Checkpoint, evt *storage.Event) error {
	atomic.AddInt64(&s.commitCalls, 1)
	if err := s.runnerTestStorage.SaveCheckpoint(ctx, cp); err != nil {
		return err
	}
	if evt != nil {
		return s.runnerTestStorage.AppendEvent(ctx, evt)
	}
	return nil
}

func (s *committerStore) AppendEvent(ctx context.Context, e *storage.Event) error {
	atomic.AddInt64(&s.appendCalled, 1)
	return s.runnerTestStorage.AppendEvent(ctx, e)
}

// When the store implements CheckpointCommitter, the runner uses the atomic
// path and does not fall back to a separate AppendEvent call.
func TestRunner_UsesCheckpointCommitter(t *testing.T) {
	store := &committerStore{runnerTestStorage: newRunnerTestStorage()}
	// Confirm the store actually satisfies the interface the runner checks.
	var _ CheckpointCommitter = store

	runner := NewRunner(buildLinearGraph("a", "b"), store)
	rs, err := runner.Run(context.Background(), "s", State{"visited": ""})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rs.Status != RunStatusCompleted {
		t.Errorf("status = %q, want completed", rs.Status)
	}
	if atomic.LoadInt64(&store.commitCalls) == 0 {
		t.Error("expected SaveCheckpointAndEvent to be used")
	}
	if atomic.LoadInt64(&store.appendCalled) != 0 {
		t.Error("runner should not call AppendEvent directly when a committer is available")
	}
}

// A pause and its subsequent resume must be visible on storage.Session.Status
// (not just the in-memory RunState), so a caller that only lists sessions —
// the CLI's session list/monitor, the dashboard — can tell a run is paused
// without loading a checkpoint.
func TestRunner_SyncsSessionStatus(t *testing.T) {
	store := newRunnerTestStorage()
	ctx := context.Background()
	if err := store.CreateSession(ctx, &storage.Session{ID: "s1", Status: "running"}); err != nil {
		t.Fatal(err)
	}

	var a, pause, b int64
	rs, err := NewRunner(buildApprovalGraph(&a, &pause, &b), store).Run(ctx, "s1", State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rs.Status != RunStatusPaused {
		t.Fatalf("run status = %q, want paused", rs.Status)
	}
	sess, err := store.GetSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != string(RunStatusPaused) {
		t.Errorf("session status after pause = %q, want %q", sess.Status, RunStatusPaused)
	}

	rs, err = NewRunner(buildApprovalGraph(&a, &pause, &b), store).Resume(ctx, "s1")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if rs.Status != RunStatusCompleted {
		t.Fatalf("run status = %q, want completed", rs.Status)
	}
	sess, err = store.GetSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != string(RunStatusCompleted) {
		t.Errorf("session status after resume = %q, want %q", sess.Status, RunStatusCompleted)
	}
}

// Against a store that implements storage.SessionStatusUpdater (e.g. the
// memory adapter), syncing the run's status must never clobber a concurrent
// Metadata write — the exact whole-record read-modify-write race a narrow
// status-only update exists to avoid.
func TestRunner_SyncSessionStatus_DoesNotClobberMetadata(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	if err := store.CreateSession(ctx, &storage.Session{ID: "s1", Status: "running"}); err != nil {
		t.Fatal(err)
	}

	var a, pause, b int64
	rs, err := NewRunner(buildApprovalGraph(&a, &pause, &b), store).Run(ctx, "s1", State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rs.Status != RunStatusPaused {
		t.Fatalf("run status = %q, want paused", rs.Status)
	}

	// Simulate a concurrent Metadata writer (e.g. the planning tool's
	// StoragePlanStore) recording state on the same session, as if it raced
	// with the pause above.
	sess, err := store.GetSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	sess.Metadata = map[string]any{"plan": "do not lose me"}
	if updateErr := store.UpdateSession(ctx, sess); updateErr != nil {
		t.Fatal(updateErr)
	}

	if _, resumeErr := NewRunner(buildApprovalGraph(&a, &pause, &b), store).Resume(ctx, "s1"); resumeErr != nil {
		t.Fatalf("Resume: %v", resumeErr)
	}

	got, err := store.GetSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != string(RunStatusCompleted) {
		t.Errorf("status = %q, want completed", got.Status)
	}
	if got.Metadata["plan"] != "do not lose me" {
		t.Errorf("Metadata = %v, want plan preserved — status sync clobbered a concurrent Metadata write", got.Metadata)
	}
}

// A run against a session the caller never persisted via CreateSession must
// still complete normally: syncing the session status is best-effort.
func TestRunner_SyncSessionStatus_MissingSessionIsNotFatal(t *testing.T) {
	store := newRunnerTestStorage()
	rs, err := NewRunner(buildLinearGraph("a", "b"), store).Run(context.Background(), "no-such-session", State{"visited": ""})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rs.Status != RunStatusCompleted {
		t.Errorf("status = %q, want completed", rs.Status)
	}
}
