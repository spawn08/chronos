package tool

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// BenchmarkExecute measures the tool dispatch hot path: lookup, permission
// check, and handler invocation (with panic-recovery deferral).
func BenchmarkExecute(b *testing.B) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:       "echo",
		Permission: PermAllow,
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			return args["x"], nil
		},
	})
	ctx := context.Background()
	args := map[string]any{"x": 42}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Execute(ctx, "echo", args); err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}

// BenchmarkExecuteParallel measures dispatch throughput under concurrent callers
// contending on the registry's RWMutex.
func BenchmarkExecuteParallel(b *testing.B) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:       "echo",
		Permission: PermAllow,
		Handler:    func(_ context.Context, args map[string]any) (any, error) { return args["x"], nil },
	})
	args := map[string]any{"x": 42}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			if _, err := r.Execute(ctx, "echo", args); err != nil {
				b.Errorf("execute: %v", err)
				return
			}
		}
	})
}

// TestRegistry_ConcurrentDispatchNoRace hammers Execute, Register, Get, and List
// concurrently to shake out races on the registry map guard. Run under -race.
func TestRegistry_ConcurrentDispatchNoRace(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:       "echo",
		Permission: PermAllow,
		Handler:    func(_ context.Context, args map[string]any) (any, error) { return args["x"], nil },
	})
	ctx := context.Background()

	var wg sync.WaitGroup
	// Executors.
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				if _, err := r.Execute(ctx, "echo", map[string]any{"x": i}); err != nil {
					t.Errorf("execute: %v", err)
					return
				}
			}
		}()
	}
	// Concurrent registrations + reads.
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				name := fmt.Sprintf("t-%d-%d", g, i)
				r.Register(&Definition{Name: name, Permission: PermAllow,
					Handler: func(context.Context, map[string]any) (any, error) { return nil, nil }})
				_, _ = r.Get(name)
				_ = r.List()
			}
		}(g)
	}
	wg.Wait()
}
