package team

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/sdk/protocol"
)

// protocolTask returns a small task payload for delegation benchmarks/stress.
func protocolTask() protocol.TaskPayload {
	return protocol.TaskPayload{
		Description: "do work",
		Input:       map[string]any{"message": "hello"},
	}
}

// TestTeam_ConcurrentDelegateNoRace fires many concurrent bus-mediated task
// delegations at a shared worker. Every caller must receive its own result with
// no mis-delivery or data race on the shared bus. Run under -race.
func TestTeam_ConcurrentDelegateNoRace(t *testing.T) {
	tm := New("del", "Delegate", StrategyCoordinator)
	tm.AddAgent(newMockAgent("worker", "done"))

	const callers = 32
	for i := 0; i < callers; i++ {
		tm.AddAgent(newMockAgent(fmt.Sprintf("caller-%d", i), "c"))
	}

	var (
		wg      sync.WaitGroup
		success atomic.Int64
	)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for c := 0; c < 20; c++ {
				res, err := tm.DelegateTask(context.Background(), fmt.Sprintf("caller-%d", i), "worker", "task", protocolTask())
				if err != nil {
					t.Errorf("delegate: %v", err)
					return
				}
				if !res.Success {
					t.Errorf("task failed: %s", res.Error)
					return
				}
				success.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if want := int64(callers * 20); success.Load() != want {
		t.Fatalf("successful delegations = %d, want %d", success.Load(), want)
	}
}

// TestTeam_ConcurrentBroadcastSharedContextNoRace stresses concurrent broadcasts
// that all write the team's SharedContext, verifying the sharedMu guard holds
// under contention. Run under -race.
func TestTeam_ConcurrentBroadcastSharedContextNoRace(t *testing.T) {
	tm := New("bcast", "Broadcast", StrategyParallel)
	for i := 0; i < 6; i++ {
		tm.AddAgent(newMockAgent(fmt.Sprintf("a%d", i), "r"))
	}

	const senders = 16
	var wg sync.WaitGroup
	for s := 0; s < senders; s++ {
		wg.Add(1)
		go func(s int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				_ = tm.Broadcast(context.Background(), "a0", "update",
					map[string]any{fmt.Sprintf("k-%d-%d", s, i): i})
			}
		}(s)
	}
	wg.Wait()
}

// TestTeam_ConcurrentRunsNoRace runs the same team from many goroutines. Each
// Run must complete without racing on the shared bus / SharedContext. Run under
// -race.
func TestTeam_ConcurrentRunsNoRace(t *testing.T) {
	tm := New("seq", "Sequential", StrategySequential)
	tm.AddAgent(newMockAgent("a1", "r1"))
	tm.AddAgent(newMockAgent("a2", "r2"))

	const goroutines = 40
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			out, err := tm.Run(context.Background(), graph.State{"message": fmt.Sprintf("m-%d", g)})
			if err != nil {
				t.Errorf("run: %v", err)
				return
			}
			if resp, _ := out["response"].(string); resp != "result-from-a2" && resp != "r2" {
				// mock agents return their configured response as "response"
				if resp == "" {
					t.Errorf("empty response from team run")
				}
			}
		}(g)
	}
	wg.Wait()
}
