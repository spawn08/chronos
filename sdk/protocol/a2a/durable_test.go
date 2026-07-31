package a2a

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/queue"
	"github.com/spawn08/chronos/storage"
	sqlitestore "github.com/spawn08/chronos/storage/adapters/sqlite"

	_ "modernc.org/sqlite" // register the sqlite driver for the queue's *sql.DB
)

// walDSN returns a WAL-mode SQLite DSN under the test temp dir, stable for the
// test's lifetime so a "restarted" component can reopen the same file.
func walDSN(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	return p + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate"
}

// newCheckpointStore opens and migrates a SQLite storage backend for task records.
func newCheckpointStore(t *testing.T, dsn string) storage.Storage {
	t.Helper()
	st, err := sqlitestore.New(dsn, sqlitestore.WithMaxOpenConns(4))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate storage: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// newQueue opens and migrates a queue over dsn.
func newQueue(t *testing.T, dsn string, cfg queue.Config) *queue.Queue {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open queue db: %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	q := queue.New(queue.NewSQLStore(db, queue.DialectSQLite), cfg)
	if err := q.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate queue: %v", err)
	}
	return q
}

// startWorker runs a fast worker driving ds.Executor until the returned cancel is
// called (registered as test cleanup).
func startWorker(t *testing.T, q *queue.Queue, ds *DurableStore, id string) {
	t.Helper()
	w, err := queue.NewWorker(q, ds.Executor, queue.WorkerConfig{
		ID:           id,
		Lease:        time.Second,
		Heartbeat:    100 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
		Backoff:      func(int) time.Duration { return 5 * time.Millisecond },
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = w.Run(ctx) }()
}

// waitForStatus polls until the task reaches a terminal state or the deadline.
func waitForStatus(t *testing.T, ctx context.Context, ds *DurableStore, id string) *Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task, err := ds.Get(ctx, id)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if isTerminal(task.Status) {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach a terminal state", id)
	return nil
}

func TestDurableRoundTrip(t *testing.T) {
	st := newCheckpointStore(t, walDSN(t, "store.db"))
	q := newQueue(t, walDSN(t, "queue.db"), queue.Config{})
	ds := NewDurableStore(q, st, echoHandler)
	startWorker(t, q, ds, "w1")

	ctx := context.Background()
	created, err := ds.Submit(ctx, "durable hello", nil)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if created.Status != TaskStatusPending {
		t.Errorf("initial status: want pending, got %s", created.Status)
	}

	final := waitForStatus(t, ctx, ds, created.ID)
	if final.Status != TaskStatusCompleted {
		t.Fatalf("final status: want completed, got %s (%s)", final.Status, final.Error)
	}
	if final.Output != "echo: durable hello" {
		t.Errorf("output: want %q, got %q", "echo: durable hello", final.Output)
	}
}

// TestDurableRestartResume submits a task while no worker is running, then starts
// a fresh queue+worker over the same durable state and asserts it resumes.
func TestDurableRestartResume(t *testing.T) {
	storeDSN := walDSN(t, "store.db")
	queueDSN := walDSN(t, "queue.db")

	st := newCheckpointStore(t, storeDSN)
	// First "process": enqueue only, no worker.
	q1 := newQueue(t, queueDSN, queue.Config{})
	ds1 := NewDurableStore(q1, st, echoHandler)

	ctx := context.Background()
	created, err := ds1.Submit(ctx, "resume me", nil)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	pending, err := ds1.Get(ctx, created.ID)
	if err != nil || pending.Status != TaskStatusPending {
		t.Fatalf("expected persisted pending task, got %+v (err=%v)", pending, err)
	}

	// Second "process": a new queue handle + worker over the same files.
	q2 := newQueue(t, queueDSN, queue.Config{})
	ds2 := NewDurableStore(q2, st, echoHandler)
	startWorker(t, q2, ds2, "w2")

	final := waitForStatus(t, ctx, ds2, created.ID)
	if final.Status != TaskStatusCompleted {
		t.Fatalf("final status: want completed, got %s", final.Status)
	}
	if final.Output != "echo: resume me" {
		t.Errorf("output: want %q, got %q", "echo: resume me", final.Output)
	}
}

// TestDurableRetryThenSucceed verifies the queue retries a failing handler and
// the task is not marked failed until the handler ultimately fails all attempts.
func TestDurableRetryThenSucceed(t *testing.T) {
	st := newCheckpointStore(t, walDSN(t, "store.db"))
	q := newQueue(t, walDSN(t, "queue.db"), queue.Config{DefaultMaxAttempts: 5})

	var mu sync.Mutex
	attempts := 0
	handler := func(_ context.Context, task *Task) error {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n < 3 {
			return fmt.Errorf("transient failure %d", n)
		}
		task.Output = "ok after retries"
		return nil
	}

	ds := NewDurableStore(q, st, handler)
	startWorker(t, q, ds, "w1")

	ctx := context.Background()
	created, err := ds.Submit(ctx, "retry", nil)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	final := waitForStatus(t, ctx, ds, created.ID)
	if final.Status != TaskStatusCompleted {
		t.Fatalf("final status: want completed, got %s (%s)", final.Status, final.Error)
	}
	mu.Lock()
	got := attempts
	mu.Unlock()
	if got != 3 {
		t.Errorf("attempts: want 3, got %d", got)
	}
}

func TestDurableTenantIsolation(t *testing.T) {
	st := newCheckpointStore(t, walDSN(t, "store.db"))
	q := newQueue(t, walDSN(t, "queue.db"), queue.Config{})
	ds := NewDurableStore(q, st, echoHandler)

	ctxA := storage.WithTenant(context.Background(), "tenant-a")
	ctxB := storage.WithTenant(context.Background(), "tenant-b")

	created, err := ds.Submit(ctxA, "secret", nil)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	if _, err := ds.Get(ctxA, created.ID); err != nil {
		t.Fatalf("owner tenant should see its task: %v", err)
	}
	if _, err := ds.Get(ctxB, created.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("cross-tenant get: want ErrTaskNotFound, got %v", err)
	}
}
