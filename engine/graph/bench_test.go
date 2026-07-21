package graph

import (
	"context"
	"fmt"
	"testing"
)

// linearGraph builds a compiled chain of n trivial nodes: entry -> n0 -> ... -> END.
// Each node increments a counter in state so the node function does real (but
// tiny) work.
func linearGraph(b *testing.B, n int) *CompiledGraph {
	b.Helper()
	g := New("bench")
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("n%d", i)
		g.AddNode(id, func(_ context.Context, s State) (State, error) {
			c, _ := s["count"].(int)
			s["count"] = c + 1
			return s, nil
		})
		if i > 0 {
			g.AddEdge(fmt.Sprintf("n%d", i-1), id)
		}
	}
	g.SetEntryPoint("n0")
	g.SetFinishPoint(fmt.Sprintf("n%d", n-1))
	cg, err := g.Compile()
	if err != nil {
		b.Fatalf("compile: %v", err)
	}
	return cg
}

// BenchmarkRunNoStore measures pure graph execution overhead (node dispatch +
// edge routing + event emit) with no durable checkpointing. A fresh Runner is
// required per run because the runner is single-use.
func BenchmarkRunNoStore(b *testing.B) {
	cg := linearGraph(b, 10)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewRunner(cg, nil)
		if _, err := r.Run(ctx, fmt.Sprintf("s-%d", i), State{"count": 0}); err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}

// BenchmarkRunWithCheckpoint measures execution including a durable checkpoint +
// ledger event per node — the production hot path.
func BenchmarkRunWithCheckpoint(b *testing.B) {
	cg := linearGraph(b, 10)
	store := newRunnerTestStorage()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewRunner(cg, store)
		if _, err := r.Run(ctx, fmt.Sprintf("s-%d", i), State{"count": 0}); err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}

// BenchmarkCompile measures graph compilation/validation, done once per graph
// but on every process start / hot-reload.
func BenchmarkCompile(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g := New("bench")
		for j := 0; j < 20; j++ {
			id := fmt.Sprintf("n%d", j)
			g.AddNode(id, func(_ context.Context, s State) (State, error) { return s, nil })
			if j > 0 {
				g.AddEdge(fmt.Sprintf("n%d", j-1), id)
			}
		}
		g.SetEntryPoint("n0")
		g.SetFinishPoint("n19")
		if _, err := g.Compile(); err != nil {
			b.Fatalf("compile: %v", err)
		}
	}
}
