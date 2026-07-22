package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSoak_SteadyStateExactlyOnce sustains a pool of workers draining a large
// backlog and asserts the queue's core guarantees under load: no work is lost
// (every run completes) and, with leases that never expire, no run is executed
// more than once. Run under -race to shake out data races in the claim path.
//
// The heavy variant is gated behind -short so the normal suite stays fast.
func TestSoak_SteadyStateExactlyOnce(t *testing.T) {
	// Timing-sensitive load test. The -short CI gate skips it (as the sibling
	// worker-churn soak does); exactly-once and no-lost-work are also covered
	// deterministically in queue_test.go / worker_test.go / sqlstore_test.go.
	if testing.Short() {
		t.Skip("skipping steady-state soak test in -short mode")
	}
	runs, workers := 2000, 16

	dsn := sqliteDSN(t)
	q := New(openStore(t, dsn), Config{})
	if err := q.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()

	for i := 0; i < runs; i++ {
		if err := q.Enqueue(ctx, &Run{SessionID: fmt.Sprintf("s-%d", i), GraphID: "g"}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	var (
		execCounts sync.Map // runID -> *atomic.Int64
		completed  atomic.Int64
	)
	exec := func(_ context.Context, r *Run) Result {
		v, _ := execCounts.LoadOrStore(r.ID, new(atomic.Int64))
		v.(*atomic.Int64).Add(1)
		completed.Add(1)
		return Result{}
	}

	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		// Each worker owns an independent store handle over the same DB, mirroring
		// distributed workers contending on shared durable state.
		wq := New(openStore(t, dsn), Config{})
		worker, err := NewWorker(wq, exec, WorkerConfig{
			ID:           fmt.Sprintf("w-%d", w),
			Lease:        30 * time.Second, // long enough to never expire mid-run
			PollInterval: 2 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("new worker: %v", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = worker.Run(runCtx)
		}()
	}

	waitForCompleted(t, q, runs, 90*time.Second)
	cancel()
	wg.Wait()

	// No lost work: every run reached the completed terminal state.
	if got := countStatus(t, q, StatusCompleted); got != runs {
		t.Fatalf("completed runs = %d, want %d (lost work)", got, runs)
	}
	if pend := pendingDepth(t, q); pend != 0 {
		t.Fatalf("pending depth = %d, want 0", pend)
	}

	// No duplicate execution: with non-expiring leases each run ran exactly once.
	distinct := 0
	execCounts.Range(func(_, v any) bool {
		distinct++
		if n := v.(*atomic.Int64).Load(); n != 1 {
			t.Errorf("run executed %d times, want 1 (duplicate execution)", n)
		}
		return true
	})
	if distinct != runs {
		t.Fatalf("distinct runs executed = %d, want %d", distinct, runs)
	}
}

// TestSoak_WorkerChurnNoLostWork stresses orphan recovery under sustained load:
// a chaos goroutine claims runs with short leases and abandons them (simulating
// workers that die mid-flight), a reaper recovers the expired leases, and a pool
// of live workers drains everything. The queue is at-least-once under failure,
// so a run may execute more than once, but NO run may be lost — every one must
// eventually reach completed.
func TestSoak_WorkerChurnNoLostWork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping worker-churn soak test in -short mode")
	}

	const (
		runs        = 300
		liveWorkers = 8
		lease       = 150 * time.Millisecond
	)

	dsn := sqliteDSN(t)
	q := New(openStore(t, dsn), Config{})
	if err := q.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()

	for i := 0; i < runs; i++ {
		if err := q.Enqueue(ctx, &Run{SessionID: fmt.Sprintf("s-%d", i), GraphID: "g", MaxAttempts: 100}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	var completed atomic.Int64
	exec := func(ctx context.Context, _ *Run) Result {
		// Simulate a little work, but honor cancellation so a lost lease aborts.
		select {
		case <-ctx.Done():
			return Result{Err: ctx.Err()}
		case <-time.After(5 * time.Millisecond):
		}
		completed.Add(1)
		return Result{}
	}

	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup

	// Live worker pool.
	for w := 0; w < liveWorkers; w++ {
		wq := New(openStore(t, dsn), Config{})
		worker, err := NewWorker(wq, exec, WorkerConfig{
			ID:           fmt.Sprintf("live-%d", w),
			Lease:        2 * time.Second,
			Heartbeat:    200 * time.Millisecond,
			PollInterval: 3 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("new worker: %v", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = worker.Run(runCtx)
		}()
	}

	// Reaper recovers leases abandoned by "dead" workers.
	reaper := NewReaper(New(openStore(t, dsn), Config{}), 40*time.Millisecond)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = reaper.Run(runCtx)
	}()

	// Chaos: claim runs with a short lease and abandon them, creating orphans.
	cq := New(openStore(t, dsn), Config{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-runCtx.Done():
				return
			default:
			}
			r, err := cq.Dequeue(runCtx, "chaos", lease)
			if errors.Is(err, ErrEmpty) || errors.Is(err, context.Canceled) {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			if err != nil {
				return
			}
			// Abandon r: never heartbeat or complete. The reaper will recover it
			// once the short lease expires.
			_ = r
			time.Sleep(20 * time.Millisecond)
		}
	}()

	waitForCompleted(t, q, runs, 60*time.Second)
	cancel()
	wg.Wait()

	if got := countStatus(t, q, StatusCompleted); got != runs {
		t.Fatalf("completed runs = %d, want %d (lost work under churn)", got, runs)
	}
	if pend := pendingDepth(t, q); pend != 0 {
		t.Fatalf("pending depth = %d, want 0", pend)
	}
	// At-least-once: total executions may exceed runs, but must cover them all.
	if got := completed.Load(); got < int64(runs) {
		t.Fatalf("executions = %d, want >= %d", got, runs)
	}
}

// waitForCompleted polls until `want` runs are completed or the deadline passes.
func waitForCompleted(t *testing.T, q *Queue, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if countStatus(t, q, StatusCompleted) >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d completed runs (have %d)", want, countStatus(t, q, StatusCompleted))
}

func countStatus(t *testing.T, q *Queue, status string) int {
	t.Helper()
	n, err := q.Store().CountByStatus(context.Background(), status)
	if err != nil {
		t.Fatalf("count %s: %v", status, err)
	}
	return n
}

func pendingDepth(t *testing.T, q *Queue) int {
	t.Helper()
	n, err := q.PendingDepth(context.Background())
	if err != nil {
		t.Fatalf("pending depth: %v", err)
	}
	return n
}
