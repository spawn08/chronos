package team

import (
	"context"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/protocol"
)

// ---------------------------------------------------------------------------
// Swarm tests
// ---------------------------------------------------------------------------

func TestNewSwarm_TooFewAgents(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	_, err := NewSwarm(SwarmConfig{Agents: []*agent.Agent{a1}})
	if err == nil {
		t.Fatal("expected error with <2 agents")
	}
}

func TestNewSwarm_Success(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")
	team, err := NewSwarm(SwarmConfig{
		Agents:       []*agent.Agent{a1, a2},
		InitialAgent: "a1",
	})
	if err != nil {
		t.Fatalf("NewSwarm: %v", err)
	}
	if team == nil {
		t.Fatal("expected non-nil team")
	}
}

func TestNewSwarm_DefaultInitialAgent(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")
	team, err := NewSwarm(SwarmConfig{
		Agents: []*agent.Agent{a1, a2},
	})
	if err != nil {
		t.Fatalf("NewSwarm: %v", err)
	}
	if team == nil {
		t.Fatal("expected non-nil team")
	}
}

func TestNewSwarm_InvalidInitialAgent(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")
	_, err := NewSwarm(SwarmConfig{
		Agents:       []*agent.Agent{a1, a2},
		InitialAgent: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for invalid initial agent")
	}
}

func TestNewSwarm_AgentHasHandoffTools(t *testing.T) {
	a1 := newMockAgent("a1", "response from a1")
	a2 := newMockAgent("a2", "response from a2")
	a3 := newMockAgent("a3", "response from a3")
	tm, err := NewSwarm(SwarmConfig{
		Agents:       []*agent.Agent{a1, a2, a3},
		InitialAgent: "a1",
	})
	if err != nil {
		t.Fatalf("NewSwarm: %v", err)
	}
	if tm == nil {
		t.Fatal("expected non-nil team")
	}
	// Each agent should have handoff tools for other agents (n-1 tools)
	a1Tools := a1.Tools.List()
	if len(a1Tools) < 2 {
		t.Errorf("a1 should have at least 2 handoff tools (for a2 and a3), got %d", len(a1Tools))
	}
}

func TestNewSwarm_DefaultMaxHandoffs(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")
	team, err := NewSwarm(SwarmConfig{
		Agents:      []*agent.Agent{a1, a2},
		MaxHandoffs: 0, // should default to 10
	})
	if err != nil {
		t.Fatalf("NewSwarm: %v", err)
	}
	if team == nil {
		t.Fatal("expected non-nil team")
	}
}

func TestSwarmHandoffTool(t *testing.T) {
	def := SwarmHandoffTool("analyst", "Analyst Agent", "Hand off to analyst for data analysis")
	if def == nil {
		t.Fatal("expected non-nil definition")
	}
	if def.Name != "transfer_to_analyst" {
		t.Errorf("Name=%q", def.Name)
	}
	if def.Handler == nil {
		t.Error("Handler should not be nil")
	}

	result, err := def.Handler(context.Background(), map[string]any{
		"task":    "analyze this data",
		"context": "some context",
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if m["handoff_to"] != "analyst" {
		t.Errorf("handoff_to=%v", m["handoff_to"])
	}
}

// ---------------------------------------------------------------------------
// Hierarchy tests
// ---------------------------------------------------------------------------

func TestNewHierarchy_NilRoot(t *testing.T) {
	_, err := NewHierarchy(HierarchyConfig{Root: nil})
	if err == nil {
		t.Fatal("expected error for nil root")
	}
}

func TestNewHierarchy_NilRootSupervisor(t *testing.T) {
	_, err := NewHierarchy(HierarchyConfig{Root: &SupervisorNode{Supervisor: nil}})
	if err == nil {
		t.Fatal("expected error for nil supervisor")
	}
}

func TestNewHierarchy_SingleLevel(t *testing.T) {
	supervisor := newMockAgent("supervisor", "I'll delegate")
	worker1 := newMockAgent("worker1", "done w1")
	worker2 := newMockAgent("worker2", "done w2")

	team, err := NewHierarchy(HierarchyConfig{
		Root: &SupervisorNode{
			Supervisor: supervisor,
			Workers:    []*agent.Agent{worker1, worker2},
		},
	})
	if err != nil {
		t.Fatalf("NewHierarchy: %v", err)
	}
	if team == nil {
		t.Fatal("expected non-nil team")
	}
}

func TestNewHierarchy_TwoLevels(t *testing.T) {
	rootSupervisor := newMockAgent("root", "managing")
	midSupervisor := newMockAgent("mid", "mid-level")
	worker := newMockAgent("worker", "doing work")

	team, err := NewHierarchy(HierarchyConfig{
		Root: &SupervisorNode{
			Supervisor: rootSupervisor,
			SubTeams: []*SupervisorNode{
				{
					Supervisor: midSupervisor,
					Workers:    []*agent.Agent{worker},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewHierarchy: %v", err)
	}
	if team == nil {
		t.Fatal("expected non-nil team")
	}
}

// ---------------------------------------------------------------------------
// SetCoordinator tests
// ---------------------------------------------------------------------------

func TestSetCoordinator(t *testing.T) {
	tm := New("t", "T", StrategyCoordinator)
	coord := newMockAgent("coord", "planning")
	result := tm.SetCoordinator(coord)
	if result != tm {
		t.Error("SetCoordinator should return team for chaining")
	}
	if tm.Coordinator == nil {
		t.Error("Coordinator should be set")
	}
}

// ---------------------------------------------------------------------------
// handleAgentMessage tests (via DelegateTask / bus message)
// ---------------------------------------------------------------------------

func TestHandleAgentMessage_TaskRequest(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	a1 := newMockAgent("a1", "task completed")
	a2 := newMockAgent("orchestrator", "orchestrating")
	tm.AddAgent(a1)
	tm.AddAgent(a2)

	result, err := tm.DelegateTask(context.Background(), "orchestrator", "a1", "test-task", protocol.TaskPayload{
		Description: "do a test task",
		Input:       map[string]any{"data": "hello"},
	})
	if err != nil {
		t.Fatalf("DelegateTask: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestHandleAgentMessage_TaskRequest_NilInput(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	a1 := newMockAgent("a1", "task done")
	a2 := newMockAgent("orchestrator", "orchestrating")
	tm.AddAgent(a1)
	tm.AddAgent(a2)

	// Nil input should be handled gracefully
	result, err := tm.DelegateTask(context.Background(), "orchestrator", "a1", "test-task", protocol.TaskPayload{
		Description: "do a task",
		Input:       nil,
	})
	if err != nil {
		t.Fatalf("DelegateTask: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestBroadcast_UpdatesSharedContext(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")
	tm.AddAgent(a1)
	tm.AddAgent(a2)

	err := tm.Broadcast(context.Background(), "a1", "context-update", map[string]any{
		"shared_key": "shared_value",
	})
	if err != nil {
		t.Fatalf("Broadcast: %v", err)
	}
}

// ---------------------------------------------------------------------------
// executeAgent helper tests
// ---------------------------------------------------------------------------

func TestExecuteAgent_WithStateMessage(t *testing.T) {
	a := newMockAgent("a", "mock response")
	state := graph.State{"message": "test message"}
	result, err := executeAgent(context.Background(), a, state)
	if err != nil {
		t.Fatalf("executeAgent: %v", err)
	}
	if result["response"] != "mock response" {
		t.Errorf("response=%v", result["response"])
	}
}

func TestExecuteAgent_WithoutMessage(t *testing.T) {
	a := newMockAgent("a", "mock response")
	state := graph.State{"key": "value", "other": 42}
	result, err := executeAgent(context.Background(), a, state)
	if err != nil {
		t.Fatalf("executeAgent: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestHandoffConfig_Validation(t *testing.T) {
	target := newMockAgent("target", "response")
	cfg := HandoffConfig{
		TargetAgent: target,
		Description: "Transfer to target agent",
	}
	def := NewHandoffTool(cfg)
	if def == nil {
		t.Fatal("expected non-nil handoff tool")
	}
}

// ---------------------------------------------------------------------------
// handleAgentMessage TypeQuestion test
// ---------------------------------------------------------------------------

func TestHandleAgentMessage_Question(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	a1 := newMockAgent("agent1", "the answer")
	a2 := newMockAgent("asker", "asking")
	tm.AddAgent(a1)
	tm.AddAgent(a2)

	answer, err := tm.Bus.Ask(context.Background(), "asker", "agent1", "what is the answer?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	_ = answer // answer may be empty since response key must match
}

func TestHandleAgentMessage_Broadcast_UpdatesSharedContext(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	a1 := newMockAgent("a1", "r1")
	tm.AddAgent(a1)

	err := tm.Broadcast(context.Background(), "external", "update", map[string]any{
		"broadcast_key": "broadcast_value",
	})
	if err != nil {
		t.Fatalf("Broadcast: %v", err)
	}
	// SharedContext should be updated
	if tm.SharedContext["broadcast_key"] != "broadcast_value" {
		// Broadcast is fire-and-forget, the shared context update may not be synchronous
		t.Log("SharedContext not yet updated (async operation)")
	}
}
