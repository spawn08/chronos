package queue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorker_RunOnceCompletes(t *testing.T) {
	s := newStore(t)
	q := New(s, Config{})
	ctx := context.Background()
	if err := q.Enqueue(ctx, &Run{SessionID: "s1"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var executed atomic.Bool
	w, err := NewWorker(q, func(ctx context.Context, r *Run) Result {
		executed.Store(true)
		return Result{}
	}, WorkerConfig{ID: "w1", Lease: time.Second})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	processed, err := w.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("run once: processed=%v err=%v", processed, err)
	}
	if !executed.Load() {
		t.Fatal("executor not invoked")
	}
}

func TestWorker_RetryThenFail(t *testing.T) {
	s := newStore(t)
	q := New(s, Config{})
	ctx := context.Background()
	// MaxAttempts=1 → first failure is terminal.
	if err := q.Enqueue(ctx, &Run{SessionID: "s1", MaxAttempts: 1}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	w, err := NewWorker(q, func(ctx context.Context, r *Run) Result {
		return Result{Err: fmt.Errorf("always fails")}
	}, WorkerConfig{ID: "w1", Lease: time.Second, Backoff: func(int) time.Duration { return 0 }})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	if _, err = w.RunOnce(ctx); err != nil {
		t.Fatalf("run once: %v", err)
	}
	failed, err := s.CountByStatus(ctx, StatusFailed)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if failed != 1 {
		t.Fatalf("want 1 failed, got %d", failed)
	}
}

// TestWorker_SleepAndParkDoNotBurnBudget is the FINDING D01 regression: a run
// that durably sleeps and parks far more times than MaxAttempts must NOT be
// failed terminally and must still complete, with attempts staying 0 across the
// healthy yields (only real failures consume the retry budget).
func TestWorker_SleepAndParkDoNotBurnBudget(t *testing.T) {
	s := newStore(t)
	q := New(s, Config{})
	ctx := context.Background()

	// Tight budget: if a sleep/park charged attempts this would fail terminally.
	run := &Run{SessionID: "s1", MaxAttempts: 2}
	if err := q.Enqueue(ctx, run); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	id := run.ID

	var yields atomic.Int64
	exec := func(ctx context.Context, r *Run) Result {
		if r.Attempts != 0 {
			t.Errorf("attempts must stay 0 across sleeps/parks, got %d", r.Attempts)
		}
		switch yields.Add(1) {
		case 1, 2, 3, 4, 5: // 5 durable sleeps, well past MaxAttempts=2
			return Result{Sleep: 2 * time.Millisecond}
		case 6: // then a HITL park
			return Result{ParkSignal: "approve"}
		default: // finally complete
			return Result{}
		}
	}
	w, err := NewWorker(q, exec, WorkerConfig{ID: "w1", Lease: time.Second})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := q.Get(ctx, id)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Status == StatusCompleted {
			break
		}
		if got.Status == StatusFailed {
			t.Fatalf("run failed terminally after healthy yields: %q", got.LastError)
		}
		if got.Status == StatusParked {
			if _, err := q.Signal(ctx, &Signal{SessionID: "s1", Name: "approve"}); err != nil {
				t.Fatalf("signal: %v", err)
			}
		}
		if _, err := w.RunOnce(ctx); err != nil {
			t.Fatalf("run once: %v", err)
		}
		time.Sleep(time.Millisecond)
	}

	got, err := q.Get(ctx, id)
	if err != nil {
		t.Fatalf("final get: %v", err)
	}
	if got.Status != StatusCompleted {
		t.Fatalf("run did not complete, status=%s", got.Status)
	}
	if got.Attempts != 0 {
		t.Fatalf("attempts want 0 after sleeps/parks, got %d", got.Attempts)
	}
	if yields.Load() < 7 {
		t.Fatalf("executor should have yielded >=7 times, got %d", yields.Load())
	}
}

// TestWorker_CrossWorkerExecution proves a run submitted by a producer is
// executed by one of several concurrent workers on independent DB handles
// (simulating separate nodes), each run exactly once.
func TestWorker_CrossWorkerExecution(t *testing.T) {
	dsn := sqliteDSN(t)
	admin := openStore(t, dsn)
	if err := admin.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	producer := New(admin, Config{})

	const runs = 40
	var (
		mu       sync.Mutex
		seen     = map[string]int{}
		executed atomic.Int64
	)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wStore := openStore(t, dsn) // independent handle == separate worker node
		wq := New(wStore, Config{})
		w, err := NewWorker(wq, func(ctx context.Context, r *Run) Result {
			mu.Lock()
			seen[r.SessionID]++
			mu.Unlock()
			executed.Add(1)
			return Result{}
		}, WorkerConfig{ID: fmt.Sprintf("w%d", i), Lease: 2 * time.Second, PollInterval: 5 * time.Millisecond})
		if err != nil {
			t.Fatalf("new worker: %v", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = w.Run(ctx)
		}()
	}

	for i := 0; i < runs; i++ {
		if err := producer.Enqueue(ctx, &Run{SessionID: fmt.Sprintf("run-%d", i)}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	waitFor(t, 10*time.Second, func() bool { return executed.Load() >= runs })
	cancel()
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != runs {
		t.Fatalf("distinct runs executed = %d, want %d", len(seen), runs)
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("run %s executed %d times, want exactly once", id, c)
		}
	}
}

// TestWorker_OrphanRecovery proves a run leased by a worker that dies mid-flight
// is recovered by another worker within the lease TTL and completed.
func TestWorker_OrphanRecovery(t *testing.T) {
	dsn := sqliteDSN(t)
	admin := openStore(t, dsn)
	if err := admin.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	q := New(admin, Config{})
	ctx := context.Background()
	if err := q.Enqueue(ctx, &Run{SessionID: "s1"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// "Dead" worker: claims the run, starts executing, then is SIGKILLed
	// (simulated by abandoning the run without heartbeating or completing).
	lease := 300 * time.Millisecond
	claimed, err := q.Dequeue(ctx, "dead-worker", lease)
	if err != nil {
		t.Fatalf("dead worker claim: %v", err)
	}
	if claimed.Status != StatusLeased {
		t.Fatalf("expected leased, got %s", claimed.Status)
	}

	// A live worker plus a reaper.
	var completed atomic.Bool
	liveStore := openStore(t, dsn)
	lq := New(liveStore, Config{})
	w, err := NewWorker(lq, func(ctx context.Context, r *Run) Result {
		completed.Store(true)
		return Result{}
	}, WorkerConfig{ID: "live-worker", Lease: 2 * time.Second, PollInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	reaper := NewReaper(New(openStore(t, dsn), Config{}), 50*time.Millisecond)

	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = w.Run(runCtx) }()
	go func() { defer wg.Done(); _ = reaper.Run(runCtx) }()

	// Within a few lease TTLs the orphan must be recovered and completed.
	waitFor(t, 5*time.Second, completed.Load)
	cancel()
	wg.Wait()

	got, err := admin.GetRun(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != StatusCompleted {
		t.Fatalf("orphan not completed: status=%s owner=%s", got.Status, got.LeaseOwner)
	}
	if got.LeaseOwner == "dead-worker" {
		t.Fatalf("run still owned by dead worker")
	}
}

// waitFor polls cond until true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
