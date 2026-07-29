package builtins

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

// newDurableStore opens a file-backed SQLite store so multiple StoragePlanStore
// instances (simulating a process restart across a resume) observe the same
// durable state.
func newDurableStore(t *testing.T) storage.Storage {
	t.Helper()
	store, err := sqlite.New(filepath.Join(t.TempDir(), "plan.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// createSession seeds the session row StoragePlanStore attaches the plan to,
// as the agent/runner run or ChatWithSession would.
func createSession(t *testing.T, ctx context.Context, store storage.Storage, id string) {
	t.Helper()
	if err := store.CreateSession(ctx, &storage.Session{
		ID: id, AgentID: "agent", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create session %q: %v", id, err)
	}
}

// The latest snapshot wins, and a fresh store instance over the same backend
// reloads it — the plan is durable across a simulated restart/resume.
func TestStoragePlanStore_PersistsAcrossFreshInstance(t *testing.T) {
	store := newDurableStore(t)
	ctx := storage.WithSession(context.Background(), "sess")
	createSession(t, ctx, store, "sess")

	writer := NewStoragePlanStore(store)
	if err := writer.Save(ctx, &Plan{Tasks: []PlanTask{
		{Content: "research", Status: TaskInProgress},
		{Content: "write", Status: TaskPending},
	}}); err != nil {
		t.Fatalf("save v1: %v", err)
	}
	// A revision marks step 1 done and starts step 2.
	if err := writer.Save(ctx, &Plan{Tasks: []PlanTask{
		{Content: "research", Status: TaskCompleted},
		{Content: "write", Status: TaskInProgress},
	}}); err != nil {
		t.Fatalf("save v2: %v", err)
	}

	// Fresh store instance = new process after resume.
	reader := NewStoragePlanStore(store)
	got, err := reader.Load(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Tasks) != 2 {
		t.Fatalf("loaded %d tasks, want 2", len(got.Tasks))
	}
	if got.Tasks[0].Status != TaskCompleted || got.Tasks[1].Status != TaskInProgress {
		t.Errorf("statuses = %q,%q, want completed,in_progress", got.Tasks[0].Status, got.Tasks[1].Status)
	}
}

// A session with no plan yet loads an empty plan (not an error).
func TestStoragePlanStore_LoadEmpty(t *testing.T) {
	store := newDurableStore(t)
	ctx := storage.WithSession(context.Background(), "empty")
	createSession(t, ctx, store, "empty")
	got, err := NewStoragePlanStore(store).Load(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Tasks) != 0 {
		t.Errorf("want empty plan, got %+v", got.Tasks)
	}
}

// A plan written by a graph node during a run survives a pause/resume, AND does
// not corrupt the runner's append-only event ledger (no duplicate seq numbers).
// Mirrors engine/graph/runner_durability_test.go's pause→resume shape.
func TestStoragePlanStore_SurvivesRunnerResumeWithoutLedgerCorruption(t *testing.T) {
	store := newDurableStore(t)
	planStore := NewStoragePlanStore(store)

	// work(normal) -> gate(interrupt) -> done. The work node records a plan.
	g := graph.New("planning-run")
	g.AddNode("work", func(ctx context.Context, s graph.State) (graph.State, error) {
		def := NewPlanTool(planStore, nil)
		_, err := def.Handler(ctx, map[string]any{"tasks": []any{
			map[string]any{"content": "collect data", "status": "completed"},
			map[string]any{"content": "await approval", "status": "in_progress"},
		}})
		return s, err
	})
	g.AddInterruptNode("gate", func(_ context.Context, s graph.State) (graph.State, error) {
		s["approved"] = true
		return s, nil
	})
	g.AddNode("done", func(_ context.Context, s graph.State) (graph.State, error) { return s, nil })
	g.SetEntryPoint("work")
	g.AddEdge("work", "gate")
	g.AddEdge("gate", "done")
	g.SetFinishPoint("done")
	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	ctx := storage.WithSession(context.Background(), "run-sess")
	createSession(t, ctx, store, "run-sess")

	rs, err := graph.NewRunner(compiled, store).Run(ctx, "run-sess", graph.State{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rs.Status != graph.RunStatusPaused {
		t.Fatalf("status = %q, want paused", rs.Status)
	}

	// After the pause, the plan is already durable.
	paused, err := NewStoragePlanStore(store).Load(ctx)
	if err != nil {
		t.Fatalf("load after pause: %v", err)
	}
	if len(paused.Tasks) != 2 {
		t.Fatalf("paused plan has %d tasks, want 2", len(paused.Tasks))
	}

	// Resume with a fresh runner; the plan must still be there afterward.
	resumed, err := graph.NewRunner(compiled, store).Resume(ctx, "run-sess")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.Status != graph.RunStatusCompleted {
		t.Errorf("resumed status = %q, want completed", resumed.Status)
	}

	got, err := NewStoragePlanStore(store).Load(ctx)
	if err != nil {
		t.Fatalf("load after resume: %v", err)
	}
	if len(got.Tasks) != 2 || got.Tasks[0].Content != "collect data" {
		t.Errorf("plan after resume = %+v, want the 2 recorded tasks", got.Tasks)
	}

	// The event ledger seq numbers must be unique: the plan store must not share
	// (and corrupt) the runner's sequence space.
	events, err := store.ListEvents(ctx, "run-sess", 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	seen := make(map[int64]bool, len(events))
	for _, e := range events {
		if seen[e.SeqNum] {
			t.Errorf("duplicate ledger seq_num %d (plan store corrupted the ledger)", e.SeqNum)
		}
		seen[e.SeqNum] = true
	}
}

// Concurrent Saves to the same session must not lose an update: the store
// serializes the read-modify-write of the session record. Run with -race.
func TestStoragePlanStore_ConcurrentSaves_SameSession(t *testing.T) {
	store := newDurableStore(t)
	ctx := storage.WithSession(context.Background(), "shared")
	createSession(t, ctx, store, "shared")
	planStore := NewStoragePlanStore(store)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if err := planStore.Save(ctx, &Plan{Tasks: []PlanTask{{Content: "x", Status: TaskInProgress}}}); err != nil {
				t.Errorf("save: %v", err)
			}
		}()
	}
	wg.Wait()

	// Whichever write won, the plan is intact (never a lost/partial record).
	got, err := planStore.Load(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].Content != "x" {
		t.Errorf("plan after concurrent saves = %+v, want a single intact task", got.Tasks)
	}
}

func BenchmarkStoragePlanStore_SaveLoad(b *testing.B) {
	store, err := sqlite.New(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		b.Fatalf("migrate: %v", err)
	}
	ctx := storage.WithSession(context.Background(), "bench")
	if err := store.CreateSession(ctx, &storage.Session{ID: "bench", AgentID: "a", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		b.Fatalf("create session: %v", err)
	}
	planStore := NewStoragePlanStore(store)
	plan := &Plan{Tasks: []PlanTask{
		{Content: "one", Status: TaskCompleted},
		{Content: "two", Status: TaskInProgress},
		{Content: "three", Status: TaskPending},
	}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := planStore.Save(ctx, plan); err != nil {
			b.Fatalf("save: %v", err)
		}
		if _, err := planStore.Load(ctx); err != nil {
			b.Fatalf("load: %v", err)
		}
	}
}
