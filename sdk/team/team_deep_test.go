package team

import (
	"context"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/sdk/agent"
)

func TestNewSwarm_TooFewAgents_Deep(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	_, err := NewSwarm(SwarmConfig{Agents: []*agent.Agent{a1}})
	if err == nil {
		t.Fatal("expected error for single agent")
	}
}

func TestNewSwarm_UnknownInitial_Deep(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")
	_, err := NewSwarm(SwarmConfig{
		Agents:       []*agent.Agent{a1, a2},
		InitialAgent: "nope",
		MaxHandoffs:  0,
	})
	if err == nil {
		t.Fatal("expected unknown initial agent error")
	}
}

func TestNewSwarm_DefaultMaxHandoffs_Deep(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")
	tm, err := NewSwarm(SwarmConfig{
		Agents:       []*agent.Agent{a1, a2},
		MaxHandoffs:  0,
		InitialAgent: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tm.Strategy != "swarm" {
		t.Fatalf("strategy %q", tm.Strategy)
	}
}

func TestSwarmHandoffTool_Handler_Deep(t *testing.T) {
	def := SwarmHandoffTool("x", "X", "desc")
	out, err := def.Handler(context.Background(), map[string]any{
		"task": "t1", "context": "c1",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["handoff_to"] != "x" || m["task"] != "t1" {
		t.Fatalf("unexpected %#v", out)
	}
}

func TestHandoffResult_MarshalFail_Deep(t *testing.T) {
	ch := make(chan int)
	_, _, err := HandoffResult(ch)
	if err == nil {
		t.Fatal("expected marshal error for channel")
	}
}

func TestSetCoordinator_BusRegistration_Deep(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	w := newMockAgent("w", "rw")
	coord := newMockAgent("c", "rc")
	tm.AddAgent(w)
	tm.SetCoordinator(coord)
	if tm.Coordinator == nil {
		t.Fatal("nil coordinator")
	}
}

func TestNewHierarchy_NilRoot_Deep(t *testing.T) {
	_, err := NewHierarchy(HierarchyConfig{})
	if err == nil {
		t.Fatal("expected nil root error")
	}
}

func TestNewHierarchy_NilRootSupervisor_Deep(t *testing.T) {
	_, err := NewHierarchy(HierarchyConfig{
		Root: &SupervisorNode{Supervisor: nil},
	})
	if err == nil {
		t.Fatal("expected nil supervisor error")
	}
}

func TestNewHierarchy_LeafSupervisorOnly_Deep(t *testing.T) {
	sup := newMockAgent("sup", "rs")
	tm, err := NewHierarchy(HierarchyConfig{
		Root: &SupervisorNode{
			Supervisor: sup,
			Workers:    nil,
			SubTeams:   nil,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tm.Strategy != "hierarchy" {
		t.Fatalf("got %q", tm.Strategy)
	}
}

func TestCollectAgents_SubMissingSupervisor_Deep(t *testing.T) {
	rootSup := newMockAgent("r", "rr")
	_, err := NewHierarchy(HierarchyConfig{
		Root: &SupervisorNode{
			Supervisor: rootSup,
			SubTeams: []*SupervisorNode{
				{Supervisor: nil, Workers: nil},
			},
		},
	})
	if err == nil {
		t.Fatal("expected collectAgents error")
	}
}

func TestTeam_UnknownStrategy_Deep(t *testing.T) {
	tm := &Team{ID: "x", Strategy: Strategy("alien")}
	_, err := tm.Run(context.Background(), graph.State{"message": "hi"})
	if err == nil {
		t.Fatal("expected unknown strategy")
	}
}
