package graph

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/spawn08/chronos/engine/queue"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

func newCheckpointStore(t *testing.T) *sqlite.Store {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "cp.db"))
	if err != nil {
		t.Fatalf("sqlite store: %v", err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate checkpoint store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func newQueue(t *testing.T) *queue.Queue {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "q.db")+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open queue db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	qs := queue.NewSQLStore(db, queue.DialectSQLite)
	q := queue.New(qs, queue.Config{})
	if err := q.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate queue: %v", err)
	}
	return q
}

// TestQueuedExecutor_RunsGraphToCompletion verifies a run enqueued by a producer
// is executed by a worker via the QueuedExecutor and drives the graph to END.
func TestQueuedExecutor_RunsGraphToCompletion(t *testing.T) {
	var sideEffects atomic.Int64
	g, err := New("linear").
		AddNode("a", func(ctx context.Context, s State) (State, error) {
			sideEffects.Add(1)
			s["a"] = true
			return s, nil
		}).
		AddNode("b", func(ctx context.Context, s State) (State, error) {
			sideEffects.Add(1)
			s["b"] = true
			return s, nil
		}).
		SetEntryPoint("a").
		AddEdge("a", "b").
		SetFinishPoint("b").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	store := newCheckpointStore(t)
	q := newQueue(t)
	exec := NewQueuedExecutor(store, SingleGraphResolver(g))

	w, err := queue.NewWorker(q, exec.Executor(),
		queue.WorkerConfig{ID: "w1", Lease: 2 * time.Second, PollInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if err = q.Enqueue(context.Background(), &queue.Run{SessionID: "sess-1", GraphID: "linear"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = w.Run(ctx) }()

	waitForRun(t, q, "", 5*time.Second, func(int) bool {
		done, _ := q.Store().CountByStatus(context.Background(), queue.StatusCompleted)
		return done == 1
	})
	cancel()
	wg.Wait()

	if got := sideEffects.Load(); got != 2 {
		t.Fatalf("side effects = %d, want 2 (each node once)", got)
	}
	cp, err := store.GetLatestCheckpoint(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if cp.NodeID != EndNode {
		t.Fatalf("final checkpoint node = %q, want %q", cp.NodeID, EndNode)
	}
}

// TestQueuedExecutor_HITLParkResume verifies the human-in-the-loop path: the
// graph pauses at an interrupt node, the worker parks the run awaiting an
// approval signal, and delivering that signal (webhook-as-signal) resumes and
// completes the run without re-running the already-executed node.
func TestQueuedExecutor_HITLParkResume(t *testing.T) {
	var approvals atomic.Int64
	g, err := New("hitl").
		AddNode("start", func(ctx context.Context, s State) (State, error) {
			s["started"] = true
			return s, nil
		}).
		AddInterruptNode("approve", func(ctx context.Context, s State) (State, error) {
			approvals.Add(1)
			s["approved"] = true
			return s, nil
		}).
		AddNode("finish", func(ctx context.Context, s State) (State, error) {
			s["done"] = true
			return s, nil
		}).
		SetEntryPoint("start").
		AddEdge("start", "approve").
		AddEdge("approve", "finish").
		SetFinishPoint("finish").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	store := newCheckpointStore(t)
	q := newQueue(t)
	exec := NewQueuedExecutor(store, SingleGraphResolver(g))
	w, err := queue.NewWorker(q, exec.Executor(),
		queue.WorkerConfig{ID: "w1", Lease: 2 * time.Second, PollInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	sessionID := "sess-hitl"
	if err = q.Enqueue(context.Background(), &queue.Run{SessionID: sessionID, GraphID: "hitl"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = w.Run(ctx) }()

	// Wait until the run is parked at the interrupt.
	waitForRun(t, q, "", 5*time.Second, func(depth int) bool {
		n, _ := q.Store().CountByStatus(context.Background(), queue.StatusParked)
		return n == 1
	})

	// Deliver the approval signal (as a HITL webhook handler would).
	n, err := q.Signal(context.Background(), &queue.Signal{SessionID: sessionID, Name: ApprovalSignal(sessionID)})
	if err != nil {
		t.Fatalf("signal: %v", err)
	}
	if n != 1 {
		t.Fatalf("signal awakened %d runs, want 1", n)
	}

	// The resumed run must complete.
	waitForRun(t, q, "", 5*time.Second, func(depth int) bool {
		done, _ := q.Store().CountByStatus(context.Background(), queue.StatusCompleted)
		return done == 1
	})
	cancel()
	wg.Wait()

	if got := approvals.Load(); got != 1 {
		t.Fatalf("interrupt node executed %d times, want exactly 1 (no double-exec on resume)", got)
	}
	cp, err := store.GetLatestCheckpoint(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if cp.NodeID != EndNode {
		t.Fatalf("final node = %q, want END", cp.NodeID)
	}
	if v, _ := cp.State["done"].(bool); !v {
		t.Fatalf("finish node did not run; state=%v", cp.State)
	}
}

// waitForRun polls a predicate against the pending queue depth until satisfied.
func waitForRun(t *testing.T, q *queue.Queue, _ string, timeout time.Duration, cond func(depth int) bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		depth, err := q.PendingDepth(context.Background())
		if err != nil {
			t.Fatalf("pending depth: %v", err)
		}
		if cond(depth) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
