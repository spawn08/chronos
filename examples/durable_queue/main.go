// Command durable_queue demonstrates the Chronos durable, distributed execution
// plane (engine/queue) delivered in Phase P1.
//
// It shows the four capabilities that make execution survive process restarts
// and scale across workers:
//
//   - Leased dequeue: multiple workers claim disjoint runs under a lease.
//   - Durable sleep: a run yields ("wait N, then continue") and is re-delivered
//     later without burning its retry budget.
//   - Park + signal (human-in-the-loop): a run parks until an external signal
//     (e.g. an approval webhook) wakes it.
//   - Orphan recovery: a reaper re-enqueues runs whose worker died mid-flight.
//
// Run it:
//
//	go run ./examples/durable_queue
//
// No API keys or network are required — the "work" is simulated so the example
// is fully self-contained and deterministic to read.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/spawn08/chronos/engine/queue"
)

// jobState is the durable payload carried by a run. It survives across sleeps,
// parks, and worker restarts because the queue persists it.
type jobState struct {
	Task  string `json:"task"`
	Stage string `json:"stage"` // "", "slept", "approved"
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("durable_queue: %v", err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Open a SQLite-backed store for the queue. Any *sql.DB works; production
	//    uses Postgres (queue.DialectPostgres) for FOR UPDATE SKIP LOCKED.
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("chronos_queue_demo_%d.db", time.Now().UnixNano()))
	defer func() { _ = os.Remove(dbPath) }()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()
	// SQLite is a single writer; one connection avoids "database is locked".
	db.SetMaxOpenConns(1)

	store := queue.NewSQLStore(db, queue.DialectSQLite)
	q := queue.New(store, queue.Config{
		MaxDepth:           100,              // admission control: park intake past this
		Policy:             queue.PolicyPark, // park (not reject) under overload
		DefaultMaxAttempts: 3,                // retry budget per run (errors only)
	})
	if err := q.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// 2. The Executor is the work function each worker runs per claimed run. It
	//    returns a Result telling the queue what to do next: complete, durably
	//    sleep, park for a signal, or fail (retryable).
	var completed sync.WaitGroup
	exec := func(ctx context.Context, r *queue.Run) queue.Result {
		var st jobState
		_ = json.Unmarshal(r.Payload, &st)

		switch st.Stage {
		case "": // first visit
			switch st.Task {
			case "report":
				// Durably sleep 1s, then continue. This does NOT consume the
				// retry budget — sleeps are intentional yields, not failures.
				fmt.Printf("  [%s] %q: sleeping 1s (durable timer)\n", r.ID, st.Task)
				st.Stage = "slept"
				return queue.Result{Sleep: time.Second, Patch: mustJSON(st)}
			case "deploy":
				// Park until a human approves via an external signal.
				fmt.Printf("  [%s] %q: parking for approval\n", r.ID, st.Task)
				st.Stage = "approved" // set the stage the signal resumes into
				return queue.Result{ParkSignal: "approve", Patch: mustJSON(st)}
			}
		case "slept":
			fmt.Printf("  [%s] %q: woke from durable sleep → done\n", r.ID, st.Task)
			completed.Done()
			return queue.Result{}
		case "approved":
			fmt.Printf("  [%s] %q: approval received → deploying → done\n", r.ID, st.Task)
			completed.Done()
			return queue.Result{}
		}
		completed.Done()
		return queue.Result{}
	}

	// 3. Start a small pool of workers plus a reaper (orphan recovery).
	wg := &sync.WaitGroup{}
	for i := 1; i <= 3; i++ {
		w, err := queue.NewWorker(q, exec, queue.WorkerConfig{
			ID:           fmt.Sprintf("worker-%d", i),
			Lease:        2 * time.Second,
			Heartbeat:    500 * time.Millisecond,
			PollInterval: 100 * time.Millisecond,
		})
		if err != nil {
			return fmt.Errorf("new worker: %w", err)
		}
		wg.Add(1)
		go func() { defer wg.Done(); _ = w.Run(ctx) }()
	}
	reaper := queue.NewReaper(q, time.Second)
	wg.Add(1)
	go func() { defer wg.Done(); _ = reaper.Run(ctx) }()

	// 4. Enqueue work. Two runs: one durable-sleep, one park-for-approval.
	completed.Add(2)
	fmt.Println("Enqueuing runs…")
	for _, task := range []string{"report", "deploy"} {
		if err := q.Enqueue(ctx, &queue.Run{
			SessionID: "sess-" + task,
			Payload:   mustJSON(jobState{Task: task}),
		}); err != nil {
			return fmt.Errorf("enqueue %s: %w", task, err)
		}
	}

	// 5. Simulate an approval webhook arriving after a moment. Signal is durable:
	//    if it arrives before the run parks, it is retained and consumed on park.
	go func() {
		time.Sleep(1500 * time.Millisecond)
		n, sigErr := q.Signal(ctx, &queue.Signal{SessionID: "sess-deploy", Name: "approve"})
		if sigErr != nil {
			log.Printf("signal: %v", sigErr)
			return
		}
		fmt.Printf("→ approval signal delivered (woke %d run)\n", n)
	}()

	// 6. Wait for both runs to finish, then shut the workers down.
	done := make(chan struct{})
	go func() { completed.Wait(); close(done) }()
	select {
	case <-done:
		fmt.Println("\n✓ All runs completed durably.")
	case <-ctx.Done():
		return fmt.Errorf("timed out waiting for runs: %w", ctx.Err())
	}

	cancel()
	wg.Wait()
	return nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // examples only: jobState always marshals
	}
	return b
}
