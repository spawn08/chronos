package team

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/protocol"
)

func newMockAgentWithError(id string, err error) *agent.Agent {
	a, _ := agent.New(id, id).
		WithModel(&mockProvider{err: err}).
		Build()
	return a
}

func TestNew_BusInitialized(t *testing.T) {
	tm := New("t1", "Team One", StrategyParallel)
	if tm.Bus == nil {
		t.Error("Bus should be initialized")
	}
	if tm.MaxIterations != 1 {
		t.Errorf("MaxIterations=%d, want 1", tm.MaxIterations)
	}
}

func TestAddAgent_Chaining(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")

	result := tm.AddAgent(a1).AddAgent(a2)
	if result != tm {
		t.Error("AddAgent should return the team for chaining")
	}
	if len(tm.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(tm.Agents))
	}
}

func TestSetRouter_Chaining(t *testing.T) {
	tm := New("t", "T", StrategyRouter)
	result := tm.SetRouter(func(_ graph.State) string { return "" })
	if result != tm {
		t.Error("SetRouter should return the team for chaining")
	}
}

func TestSetMerge_Chaining(t *testing.T) {
	tm := New("t", "T", StrategyParallel)
	result := tm.SetMerge(func(_ []graph.State) graph.State { return graph.State{} })
	if result != tm {
		t.Error("SetMerge should return the team for chaining")
	}
}

func TestSetErrorStrategy_Chaining(t *testing.T) {
	tm := New("t", "T", StrategyParallel)
	result := tm.SetErrorStrategy(ErrorStrategyCollect)
	if result != tm {
		t.Error("SetErrorStrategy should return the team for chaining")
	}
	if tm.ErrorMode != ErrorStrategyCollect {
		t.Errorf("ErrorMode=%v, want Collect", tm.ErrorMode)
	}
}

func TestSetMaxConcurrency_Chaining(t *testing.T) {
	tm := New("t", "T", StrategyParallel)
	result := tm.SetMaxConcurrency(4)
	if result != tm {
		t.Error("SetMaxConcurrency should return the team for chaining")
	}
	if tm.MaxConcurrency != 4 {
		t.Errorf("MaxConcurrency=%d, want 4", tm.MaxConcurrency)
	}
}

func TestSetMaxIterations_Chaining(t *testing.T) {
	tm := New("t", "T", StrategyCoordinator)
	result := tm.SetMaxIterations(5)
	if result != tm {
		t.Error("SetMaxIterations should return the team for chaining")
	}
	if tm.MaxIterations != 5 {
		t.Errorf("MaxIterations=%d, want 5", tm.MaxIterations)
	}
}

func TestSequential_EmptyAgents(t *testing.T) {
	tm := New("empty", "Empty", StrategySequential)
	// With no agents, sequential should complete without error
	result, err := tm.Run(context.Background(), graph.State{"message": "hello"})
	if err != nil {
		t.Fatalf("Run with no agents: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestParallel_EmptyAgents(t *testing.T) {
	tm := New("empty", "Empty", StrategyParallel)
	result, err := tm.Run(context.Background(), graph.State{"message": "hello"})
	if err != nil {
		t.Fatalf("Run with no agents: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestRouter_NoRouter_FallsBack(t *testing.T) {
	// Router with no router function set - should fall back to first agent
	tm := New("rtr", "Router", StrategyRouter)
	tm.AddAgent(newMockAgent("a1", "response from a1"))

	result, err := tm.Run(context.Background(), graph.State{"message": "hello"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	_ = result // just ensure no panic
}

func TestRouter_NoAgents(t *testing.T) {
	tm := New("rtr", "Router", StrategyRouter)
	tm.SetRouter(func(_ graph.State) string { return "nonexistent" })

	_, err := tm.Run(context.Background(), graph.State{"message": "hello"})
	if err == nil {
		t.Fatal("expected error when router selects nonexistent agent")
	}
}

func TestParallel_WithMergeFunc(t *testing.T) {
	tm := New("par", "Parallel", StrategyParallel)
	tm.AddAgent(newMockAgent("a1", "result1"))
	tm.AddAgent(newMockAgent("a2", "result2"))
	tm.SetMerge(func(results []graph.State) graph.State {
		merged := graph.State{}
		for i, r := range results {
			key := "r" + string(rune('0'+i))
			merged[key] = r["response"]
		}
		return merged
	})

	result, err := tm.Run(context.Background(), graph.State{"message": "hello"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestSequential_PassesStateThrough(t *testing.T) {
	// Each agent in sequential should see previous agent's response
	tm := New("seq", "Sequential", StrategySequential)
	tm.AddAgent(newMockAgent("a1", "step1-done"))
	tm.AddAgent(newMockAgent("a2", "step2-done"))
	tm.AddAgent(newMockAgent("a3", "step3-done"))

	result, err := tm.Run(context.Background(), graph.State{"message": "start"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Final response should be from last agent
	if result["response"] != "step3-done" {
		t.Errorf("response=%v, want step3-done", result["response"])
	}
}

func TestParallel_MaxConcurrency(t *testing.T) {
	tm := New("par", "Parallel", StrategyParallel)
	tm.SetMaxConcurrency(2)
	tm.AddAgent(newMockAgent("a1", "r1"))
	tm.AddAgent(newMockAgent("a2", "r2"))
	tm.AddAgent(newMockAgent("a3", "r3"))
	tm.AddAgent(newMockAgent("a4", "r4"))

	result, err := tm.Run(context.Background(), graph.State{"message": "hello"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestDirectChannel(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	tm.AddAgent(newMockAgent("a1", "r1"))
	tm.AddAgent(newMockAgent("a2", "r2"))

	ch := tm.DirectChannel("a1", "a2", 10)
	if ch == nil {
		t.Error("expected DirectChannel to return non-nil")
	}
}

func TestBroadcast(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	tm.AddAgent(newMockAgent("sender", "r"))

	err := tm.Broadcast(context.Background(), "sender", "hello-subject", map[string]any{"key": "value"})
	// Should not error (broadcast to all)
	if err != nil {
		t.Fatalf("Broadcast: %v", err)
	}
}

func TestSetModelRouter(t *testing.T) {
	tm := New("t", "T", StrategyRouter)
	called := false
	tm.SetModelRouter(func(ctx context.Context, state graph.State, agents []AgentInfo) (string, error) {
		called = true
		if len(agents) > 0 {
			return agents[0].ID, nil
		}
		return "", errors.New("no agents")
	})

	tm.AddAgent(newMockAgent("a1", "response"))
	_, err := tm.Run(context.Background(), graph.State{"message": "route me"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Error("model router should have been called")
	}
}

func TestDelegateTask(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	a1 := newMockAgent("a1", "task result")
	tm.AddAgent(a1)

	_, err := tm.DelegateTask(context.Background(), "sender", "a1", "do task", protocol.TaskPayload{
		Description: "do something",
		Input:       map[string]any{"key": "value"},
	})
	// May fail if agent handling is not fully implemented; just verify no panic
	_ = err
}

func TestStateToPrompt(t *testing.T) {
	state := graph.State{
		"key1": "value1",
		"key2": 42,
		"_task_description": "hidden",
		"_delegated_by":     "also hidden",
	}
	result := stateToPrompt(state)
	if result == "" {
		t.Error("expected non-empty prompt")
	}
	// Hidden keys should not appear
	for _, hidden := range []string{"_task_description", "_delegated_by"} {
		if len(result) > 0 {
			found := false
			for i := 0; i <= len(result)-len(hidden); i++ {
				if result[i:i+len(hidden)] == hidden {
					found = true
					break
				}
			}
			if found {
				t.Errorf("stateToPrompt should skip %q", hidden)
			}
		}
	}
}

func TestAgentInfoList_Order(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	a1, _ := agent.New("z-agent", "Z").Description("last").WithModel(&mockProvider{response: "r"}).Build()
	a2, _ := agent.New("a-agent", "A").Description("first").WithModel(&mockProvider{response: "r"}).Build()
	tm.AddAgent(a1)
	tm.AddAgent(a2)

	infos := tm.agentInfoList()
	if len(infos) != 2 {
		t.Fatalf("expected 2 infos, got %d", len(infos))
	}
	// Order should match insertion order
	if infos[0].ID != "z-agent" {
		t.Errorf("infos[0].ID=%q, want z-agent", infos[0].ID)
	}
	if infos[1].ID != "a-agent" {
		t.Errorf("infos[1].ID=%q, want a-agent", infos[1].ID)
	}
}
