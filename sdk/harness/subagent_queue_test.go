package harness

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/queue"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"

	_ "modernc.org/sqlite"
)

// queueSystem wires the durable pieces a QueuedRunner needs: a checkpoint store,
// a queue, the shared subagent graph, and a QueuedExecutor to attach to workers.
type queueSystem struct {
	store storage.Storage
	q     *queue.Queue
	exec  queue.Executor
	svc   *SubAgentService
}

func newQueueSystem(t *testing.T, subReply string) *queueSystem {
	t.Helper()

	store, err := sqlite.New(filepath.Join(t.TempDir(), "cp.db"))
	if err != nil {
		t.Fatalf("open checkpoint store: %v", err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate checkpoint store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	dsn := filepath.Join(t.TempDir(), "queue.db") + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open queue db: %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	qstore := queue.NewSQLStore(db, queue.DialectSQLite)
	if err = qstore.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate queue: %v", err)
	}
	q := queue.New(qstore, queue.Config{})

	parent := newParent(t, &recordingMock{name: "parent"})
	svc, err := NewSubAgentService(parent)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	svc.model = &recordingMock{name: "sub", reply: subReply}
	if err = svc.Register(SubAgentSpec{Name: "worker", SystemPrompt: "You do work."}); err != nil {
		t.Fatalf("register: %v", err)
	}

	g, err := NewSubAgentGraph(svc)
	if err != nil {
		t.Fatalf("subagent graph: %v", err)
	}
	exec := graph.NewQueuedExecutor(store, graph.SingleGraphResolver(g)).Executor()

	return &queueSystem{store: store, q: q, exec: exec, svc: svc}
}

// A subagent runs to completion through the durable queue and its result is read
// back from the final checkpoint.
func TestQueuedRunner_EndToEnd(t *testing.T) {
	sys := newQueueSystem(t, "durable-result")

	worker, err := queue.NewWorker(sys.q, sys.exec, queue.WorkerConfig{
		ID: "w1", Lease: 2 * time.Second, PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = worker.Run(ctx) }()

	runner := NewQueuedRunner(sys.svc, sys.q, sys.store, WithPollInterval(10*time.Millisecond))
	spec, _ := sys.svc.resolve("worker", SubAgentSpec{})

	runCtx, runCancel := context.WithTimeout(ctx, 5*time.Second)
	defer runCancel()
	result, err := runner.Run(runCtx, spec, "do the thing")
	if err != nil {
		t.Fatalf("queued run: %v", err)
	}
	if result != "durable-result" {
		t.Errorf("result = %q, want durable-result", result)
	}

	cancel()
	wg.Wait()
}

// Dynamic (unregistered) subagents cannot be run durably: a remote worker could
// not reconstruct them.
func TestQueuedRunner_RejectsDynamic(t *testing.T) {
	sys := newQueueSystem(t, "x")
	runner := NewQueuedRunner(sys.svc, sys.q, sys.store)
	_, err := runner.Run(context.Background(), SubAgentSpec{Name: "adhoc", SystemPrompt: "p"}, "t")
	if err == nil {
		t.Fatal("expected queued runner to reject an unregistered (dynamic) subagent")
	}
}

// A subagent run leased by a worker that dies mid-flight is recovered by another
// worker and completed, with its result durably available — the subagent is
// resumable across worker death. Mirrors engine/queue TestWorker_OrphanRecovery.
func TestQueuedSubAgent_OrphanRecovery(t *testing.T) {
	sys := newQueueSystem(t, "recovered-result")
	ctx := context.Background()

	sessionID := "subagent_orphan"
	payload, err := json.Marshal(graph.RunPayload{Initial: graph.State{
		stateSubAgentName: "worker",
		stateSubAgentTask: "do it",
	}})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err = sys.q.Enqueue(ctx, &queue.Run{
		ID: sessionID, SessionID: sessionID, GraphID: SubAgentGraphID, Kind: queue.KindStart, Payload: payload,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// A worker claims the run under a short lease, then "dies" (never executes,
	// heartbeats, or completes it).
	if _, err = sys.q.Dequeue(ctx, "dead-worker", 300*time.Millisecond); err != nil {
		t.Fatalf("dead claim: %v", err)
	}

	// A live worker plus a reaper recover and complete the orphan.
	worker, err := queue.NewWorker(sys.q, sys.exec, queue.WorkerConfig{
		ID: "live", Lease: 2 * time.Second, PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	reaper := queue.NewReaper(sys.q, 50*time.Millisecond)

	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = worker.Run(runCtx) }()
	go func() { defer wg.Done(); _ = reaper.Run(runCtx) }()

	// Within a few lease TTLs the orphan is recovered and completed.
	deadline := time.After(6 * time.Second)
	for {
		run, getErr := sys.q.Get(ctx, sessionID)
		if getErr != nil {
			t.Fatalf("get run: %v", getErr)
		}
		if run.Status == queue.StatusCompleted {
			break
		}
		if run.Status == queue.StatusFailed {
			t.Fatalf("run failed: %s", run.LastError)
		}
		select {
		case <-deadline:
			t.Fatalf("orphan not recovered in time (status=%s)", run.Status)
		case <-time.After(20 * time.Millisecond):
		}
	}

	cancel()
	wg.Wait()

	// The recovered subagent's result is durably available.
	cp, err := sys.store.GetLatestCheckpoint(storage.WithSession(ctx, sessionID), sessionID)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if got, _ := cp.State[stateSubAgentOut].(string); got != "recovered-result" {
		t.Errorf("recovered result = %q, want recovered-result", got)
	}
}
