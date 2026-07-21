package graph

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// TestRunner_ConcurrentRunsSharedStoreNoRace runs many independent graph
// executions concurrently against a single shared store. Each Runner is
// single-use and drives its own session, so correct behavior requires no shared
// mutable runner state to leak across executions and the store to be safe under
// concurrent checkpoint writes. Run under -race.
func TestRunner_ConcurrentRunsSharedStoreNoRace(t *testing.T) {
	g := New("stress")
	g.AddNode("a", func(_ context.Context, s State) (State, error) {
		s["a"] = true
		return s, nil
	})
	g.AddNode("b", func(_ context.Context, s State) (State, error) {
		s["b"] = true
		return s, nil
	})
	g.AddEdge("a", "b")
	g.SetEntryPoint("a")
	g.SetFinishPoint("b")
	cg, err := g.Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	store := newRunnerTestStorage()
	ctx := context.Background()

	const goroutines = 64
	var (
		wg        sync.WaitGroup
		completed atomic.Int64
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := NewRunner(cg, store)
			rs, err := r.Run(ctx, fmt.Sprintf("sess-%d", i), State{"i": i})
			if err != nil {
				t.Errorf("run %d: %v", i, err)
				return
			}
			if rs.Status != RunStatusCompleted {
				t.Errorf("run %d status = %s, want completed", i, rs.Status)
				return
			}
			completed.Add(1)
		}(i)
	}
	wg.Wait()

	if completed.Load() != goroutines {
		t.Fatalf("completed = %d, want %d", completed.Load(), goroutines)
	}
}

// TestRunner_ConcurrentStreamConsumersNoRace verifies that a run's event stream
// can be consumed concurrently with execution without racing on the runner's
// channel/close bookkeeping. Run under -race.
func TestRunner_ConcurrentStreamConsumersNoRace(t *testing.T) {
	g := New("stream")
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("n%d", i)
		g.AddNode(id, func(_ context.Context, s State) (State, error) { return s, nil })
		if i > 0 {
			g.AddEdge(fmt.Sprintf("n%d", i-1), id)
		}
	}
	g.SetEntryPoint("n0")
	g.SetFinishPoint("n4")
	cg, err := g.Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	const goroutines = 32
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := NewRunner(cg, nil)

			var drain sync.WaitGroup
			drain.Add(1)
			go func() {
				defer drain.Done()
				for range r.Stream() { // drain until closed
				}
			}()

			if _, err := r.Run(context.Background(), fmt.Sprintf("s-%d", i), State{}); err != nil {
				t.Errorf("run %d: %v", i, err)
			}
			drain.Wait()
		}(i)
	}
	wg.Wait()
}
