package team

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/sdk/agent"
)

func TestNewSwarm_Errors_Table(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")

	tests := []struct {
		name    string
		cfg     SwarmConfig
		wantSub string
	}{
		{
			name:    "too_few_agents",
			cfg:     SwarmConfig{Agents: []*agent.Agent{a1}},
			wantSub: "at least 2 agents",
		},
		{
			name: "initial_not_found",
			cfg: SwarmConfig{
				Agents:       []*agent.Agent{a1, a2},
				InitialAgent: "missing",
			},
			wantSub: `initial agent "missing" not found`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSwarm(tt.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("err=%v, want substring %q", err, tt.wantSub)
			}
		})
	}
}

func TestNewSwarm_PanicsWhenToolRegistryNil(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")
	a1.Tools = nil
	a2.Tools = nil

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when Tools is nil during handoff wiring")
		}
	}()
	_, _ = NewSwarm(SwarmConfig{Agents: []*agent.Agent{a1, a2}})
}

func TestRunParallel_FailFast(t *testing.T) {
	tm := New("p", "P", StrategyParallel)
	tm.SetErrorStrategy(ErrorStrategyFailFast)
	tm.AddAgent(newMockAgentWithError("a1", errors.New("boom")))
	tm.AddAgent(newMockAgent("a2", "ok"))

	_, err := tm.Run(context.Background(), graph.State{"message": "hi"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunParallel_ContextCancelled(t *testing.T) {
	tm := New("p2", "P2", StrategyParallel)
	tm.AddAgent(newMockAgent("a1", "r1"))
	tm.AddAgent(newMockAgent("a2", "r2"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tm.Run(ctx, graph.State{"message": "hi"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunParallel_CollectMode(t *testing.T) {
	tm := New("p3", "P3", StrategyParallel)
	tm.SetErrorStrategy(ErrorStrategyCollect)
	tm.AddAgent(newMockAgentWithError("a1", errors.New("e1")))
	tm.AddAgent(newMockAgentWithError("a2", errors.New("e2")))

	_, err := tm.Run(context.Background(), graph.State{"message": "hi"})
	if err == nil {
		t.Fatal("expected combined error")
	}
	if !strings.Contains(err.Error(), "2 agents failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRouter_ModelRouterError(t *testing.T) {
	tm := New("rt", "RT", StrategyRouter)
	tm.AddAgent(newMockAgent("w", "ok"))
	tm.SetModelRouter(func(context.Context, graph.State, []AgentInfo) (string, error) {
		return "", errors.New("router failed")
	})

	_, err := tm.Run(context.Background(), graph.State{"message": "do"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunRouter_StaticRouterEmptyID(t *testing.T) {
	tm := New("rt2", "RT2", StrategyRouter)
	tm.AddAgent(newMockAgent("w", "ok"))
	tm.SetRouter(func(graph.State) string { return "" })

	_, err := tm.Run(context.Background(), graph.State{"message": "do"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunRouter_UnknownSelectedAgent(t *testing.T) {
	tm := New("rt3", "RT3", StrategyRouter)
	tm.AddAgent(newMockAgent("w", "ok"))
	tm.SetRouter(func(graph.State) string { return "ghost" })

	_, err := tm.Run(context.Background(), graph.State{"message": "do"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunRouter_CapabilityMatchNoAgents(t *testing.T) {
	tm := New("rt4", "RT4", StrategyRouter)

	_, err := tm.Run(context.Background(), graph.State{"message": "do"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no agents registered") {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestRunRouter_AgentExecuteError(t *testing.T) {
	tm := New("rt5", "RT5", StrategyRouter)
	tm.AddAgent(newMockAgentWithError("bad", errors.New("exec fail")))
	tm.SetRouter(func(graph.State) string { return "bad" })

	_, err := tm.Run(context.Background(), graph.State{"message": "do"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunRouter_ModelRouterUnknownAgent(t *testing.T) {
	tm := New("mr", "MR", StrategyRouter)
	tm.AddAgent(newMockAgent("real", "ok"))
	tm.SetModelRouter(func(context.Context, graph.State, []AgentInfo) (string, error) {
		return "nope", nil
	})

	_, err := tm.Run(context.Background(), graph.State{"message": "hi"})
	if err == nil {
		t.Fatal("expected error for unknown routed agent")
	}
}

func TestRunSequential_FirstAgentFails(t *testing.T) {
	tm := New("s", "S", StrategySequential)
	tm.AddAgent(newMockAgentWithError("a1", errors.New("first")))
	tm.AddAgent(newMockAgent("a2", "second"))

	_, err := tm.Run(context.Background(), graph.State{"message": "hi"})
	if err == nil {
		t.Fatal("expected error from first agent")
	}
}

func TestRunSequential_ContextCancelledBeforeRun(t *testing.T) {
	tm := New("s2", "S2", StrategySequential)
	tm.AddAgent(newMockAgent("a1", "r1"))
	tm.AddAgent(newMockAgent("a2", "r2"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tm.Run(ctx, graph.State{"message": "hi"})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestCoordinatorPlan_InvalidJSON(t *testing.T) {
	tm := New("c", "C", StrategyCoordinator)
	badJSON := &mockProvider{response: "this is not {{{ json"}
	coord, _ := agent.New("coord", "coord").WithModel(badJSON).Build()
	w := newMockAgent("w", "ok")
	tm.AddAgent(coord)
	tm.AddAgent(w)
	tm.SetCoordinator(coord)

	_, err := tm.Run(context.Background(), graph.State{"message": "task"})
	if err == nil {
		t.Fatal("expected error from invalid coordinator JSON")
	}
}

func TestCoordinatorPlan_UnknownAgentInPlan(t *testing.T) {
	tm := New("c2", "C2", StrategyCoordinator)
	prov := &mockProvider{response: `{"tasks":[{"agent_id":"nobody","description":"x"}],"done":false}`}
	coord, _ := agent.New("coord", "coord").WithModel(prov).Build()
	tm.AddAgent(coord)
	tm.AddAgent(newMockAgent("w", "w"))
	tm.SetCoordinator(coord)

	_, err := tm.Run(context.Background(), graph.State{"message": "task"})
	if err == nil {
		t.Fatal("expected error for unknown agent in plan")
	}
}

func TestRunCoordinator_ExecutePlanDelegateFailure(t *testing.T) {
	tm := New("c3", "C3", StrategyCoordinator)
	prov := &mockProvider{response: `{"tasks":[{"agent_id":"w","description":"work"}],"done":false}`}
	coord, _ := agent.New("coord", "coord").WithModel(prov).Build()
	tm.AddAgent(coord)
	tm.AddAgent(newMockAgentWithError("w", errors.New("worker failed")))
	tm.SetCoordinator(coord)

	_, err := tm.Run(context.Background(), graph.State{"message": "task"})
	if err == nil {
		t.Fatal("expected error from task execution")
	}
}

func TestRunCoordinator_PlanIterationError(t *testing.T) {
	tm := New("c4", "C4", StrategyCoordinator)
	errProv := &mockProvider{err: errors.New("plan model down")}
	coord, _ := agent.New("coord", "coord").WithModel(errProv).Build()
	tm.AddAgent(coord)
	tm.AddAgent(newMockAgent("w", "ok"))
	tm.SetCoordinator(coord)

	_, err := tm.Run(context.Background(), graph.State{"message": "task"})
	if err == nil {
		t.Fatal("expected error from coordinator plan")
	}
}

func TestSetCoordinator_OnEmptyTeam(t *testing.T) {
	tm := New("e", "E", StrategyCoordinator)
	coord := newMockAgent("coord", `{"tasks":[],"done":true}`)
	tm.SetCoordinator(coord)

	_, err := tm.Run(context.Background(), graph.State{"message": "x"})
	if err == nil {
		t.Fatal("expected error: no agents in Order")
	}
}

func TestCompile_EdgeTargetMissing(t *testing.T) {
	// Mirrors failure mode when an edge references a non-existent node (buildHierarchyGraph safety net at compile time).
	g := graph.New("bad-edge")
	g.AddNode("only", func(ctx context.Context, s graph.State) (graph.State, error) { return s, nil })
	g.SetEntryPoint("only")
	g.AddEdge("only", "missing-node")
	_, err := g.Compile()
	if err == nil {
		t.Fatal("expected compile error for missing edge target")
	}
}
