package team

import (
	"context"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
)

// BenchmarkSequentialRun measures a sequential team pipeline of mock agents.
func BenchmarkSequentialRun(b *testing.B) {
	tm := New("seq", "Sequential", StrategySequential)
	tm.AddAgent(newMockAgent("a1", "r1"))
	tm.AddAgent(newMockAgent("a2", "r2"))
	tm.AddAgent(newMockAgent("a3", "r3"))
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tm.Run(ctx, graph.State{"message": "hello"}); err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}

// BenchmarkParallelRun measures a parallel fan-out/fan-in team of mock agents.
func BenchmarkParallelRun(b *testing.B) {
	tm := New("par", "Parallel", StrategyParallel)
	tm.AddAgent(newMockAgent("a1", "r1"))
	tm.AddAgent(newMockAgent("a2", "r2"))
	tm.AddAgent(newMockAgent("a3", "r3"))
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tm.Run(ctx, graph.State{"message": "hello"}); err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}

// BenchmarkDelegateTask measures a single bus-mediated task delegation
// round-trip between two agents.
func BenchmarkDelegateTask(b *testing.B) {
	tm := New("del", "Delegate", StrategyCoordinator)
	tm.AddAgent(newMockAgent("caller", "c"))
	tm.AddAgent(newMockAgent("worker", "done"))
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tm.DelegateTask(ctx, "caller", "worker", "task", protocolTask()); err != nil {
			b.Fatalf("delegate: %v", err)
		}
	}
}
