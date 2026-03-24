package team

import (
	"context"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/sdk/agent"
)

// TestBuildHierarchyGraph_LeafSupervisor tests the path where a supervisor
// has no workers or sub-teams, becoming a leaf node with SetFinishPoint.
func TestBuildHierarchyGraph_LeafSupervisor(t *testing.T) {
	// A supervisor with no workers or sub-teams is a leaf
	leafSupervisor := newMockAgent("leaf", "leaf response")

	team, err := NewHierarchy(HierarchyConfig{
		Root: &SupervisorNode{
			Supervisor: leafSupervisor,
			Workers:    nil,
			SubTeams:   nil,
		},
	})
	if err != nil {
		t.Fatalf("NewHierarchy with leaf supervisor: %v", err)
	}
	if team == nil {
		t.Fatal("expected non-nil team")
	}
	if team.Strategy != "hierarchy" {
		t.Errorf("Strategy = %q, want hierarchy", team.Strategy)
	}
}

// TestBuildHierarchyGraph_OnlySubTeams tests a supervisor with only sub-teams
// and no direct workers.
func TestBuildHierarchyGraph_OnlySubTeams(t *testing.T) {
	root := newMockAgent("root", "root response")
	sub1Super := newMockAgent("sub1-super", "sub1 response")
	sub1Worker := newMockAgent("sub1-worker", "worker response")

	team, err := NewHierarchy(HierarchyConfig{
		Root: &SupervisorNode{
			Supervisor: root,
			SubTeams: []*SupervisorNode{
				{
					Supervisor: sub1Super,
					Workers:    []*agent.Agent{sub1Worker},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewHierarchy with sub-teams only: %v", err)
	}
	if team == nil {
		t.Fatal("expected non-nil team")
	}
}

// TestBuildHierarchyGraph_MultipleWorkers tests a supervisor with multiple workers.
func TestBuildHierarchyGraph_MultipleWorkers(t *testing.T) {
	supervisor := newMockAgent("supervisor", "supervising")
	workers := []*agent.Agent{
		newMockAgent("w1", "work1"),
		newMockAgent("w2", "work2"),
		newMockAgent("w3", "work3"),
	}

	team, err := NewHierarchy(HierarchyConfig{
		Root: &SupervisorNode{
			Supervisor: supervisor,
			Workers:    workers,
		},
	})
	if err != nil {
		t.Fatalf("NewHierarchy with multiple workers: %v", err)
	}
	if team == nil {
		t.Fatal("expected non-nil team")
	}
	// All agents should be in the map
	if len(team.Agents) != 4 { // 1 supervisor + 3 workers
		t.Errorf("expected 4 agents, got %d", len(team.Agents))
	}
}

// TestCollectAgents_NilSupervisorInSubTeam tests error when sub-team has nil supervisor.
func TestCollectAgents_NilSupervisorInSubTeam(t *testing.T) {
	root := newMockAgent("root", "root")
	_, err := NewHierarchy(HierarchyConfig{
		Root: &SupervisorNode{
			Supervisor: root,
			SubTeams: []*SupervisorNode{
				{Supervisor: nil}, // nil supervisor in sub-team
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for nil supervisor in sub-team")
	}
}

// TestNewSwarm_MaxHandoffsHonored tests the max handoffs configuration.
func TestNewSwarm_MaxHandoffs(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")
	team, err := NewSwarm(SwarmConfig{
		Agents:      []*agent.Agent{a1, a2},
		MaxHandoffs: 5,
	})
	if err != nil {
		t.Fatalf("NewSwarm: %v", err)
	}
	if team == nil {
		t.Fatal("expected non-nil team")
	}
}

// TestHierarchyAgentMap_ContainsAll verifies all agents are accessible from the team.
func TestHierarchyAgentMap_ContainsAll(t *testing.T) {
	root := newMockAgent("root", "r")
	sub1 := newMockAgent("sub1", "s1")
	worker1 := newMockAgent("worker1", "w1")
	worker2 := newMockAgent("worker2", "w2")

	team, err := NewHierarchy(HierarchyConfig{
		Root: &SupervisorNode{
			Supervisor: root,
			Workers:    []*agent.Agent{worker1},
			SubTeams: []*SupervisorNode{
				{
					Supervisor: sub1,
					Workers:    []*agent.Agent{worker2},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewHierarchy: %v", err)
	}

	expectedAgents := []string{"root", "worker1", "sub1", "worker2"}
	for _, id := range expectedAgents {
		if _, ok := team.Agents[id]; !ok {
			t.Errorf("agent %q not found in team.Agents", id)
		}
	}
}

// TestSwarmHandoffTool_NoContext tests calling handoff tool without context.
func TestSwarmHandoffTool_NoContext(t *testing.T) {
	def := SwarmHandoffTool("coder", "Coder Agent", "Hand off coding tasks")
	if def == nil {
		t.Fatal("expected non-nil definition")
	}

	result, err := def.Handler(context.Background(), map[string]any{
		"task": "write unit tests",
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result")
	}
	if m["handoff_to"] != "coder" {
		t.Errorf("handoff_to = %v, want coder", m["handoff_to"])
	}
}

// TestBuildHierarchyGraph_DirectCall tests calling buildHierarchyGraph directly
// with a leaf node to ensure the SetFinishPoint path is covered.
func TestBuildHierarchyGraph_DirectCall(t *testing.T) {
	g := graph.New("test-hierarchy")
	supervisor := newMockAgent("solo", "solo response")
	node := &SupervisorNode{
		Supervisor: supervisor,
	}
	buildHierarchyGraph(g, node)
	g.SetEntryPoint(supervisor.ID)
	// Compiling verifies the graph is valid (finish point is set by buildHierarchyGraph for leaf)
	_, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
}
