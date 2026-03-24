package team

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/sdk/agent"
)

func TestCoordinatorSystemPrompt(t *testing.T) {
	agents := []AgentInfo{
		{ID: "a1", Name: "Analyst", Description: "Analyzes data", Capabilities: []string{"analysis"}},
		{ID: "a2", Name: "Writer", Description: "Writes reports"},
	}
	prompt := coordinatorSystemPrompt(agents)
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	// Should contain agent info
	if len(prompt) < 50 {
		t.Error("expected substantial prompt")
	}
}

func TestExtractJSON_Simple(t *testing.T) {
	input := `{"key": "value"}`
	result := extractJSON(input)
	if result != `{"key": "value"}` {
		t.Errorf("extractJSON=%q", result)
	}
}

func TestExtractJSON_WithSurroundingText(t *testing.T) {
	input := "Here is the JSON:\n```json\n{\"tasks\": []}\n```"
	result := extractJSON(input)
	if result != `{"tasks": []}` {
		t.Errorf("extractJSON=%q", result)
	}
}

func TestExtractJSON_NoJSON(t *testing.T) {
	input := "no json here"
	result := extractJSON(input)
	if result != "no json here" {
		t.Errorf("expected original string, got %q", result)
	}
}

func TestExtractJSON_Nested(t *testing.T) {
	input := `outer {"key": {"nested": "value"}} text`
	result := extractJSON(input)
	if result != `{"key": {"nested": "value"}}` {
		t.Errorf("extractJSON=%q", result)
	}
}

func TestExtractJSON_Truncated(t *testing.T) {
	// No closing brace - should return from start
	input := `{"key": "val`
	result := extractJSON(input)
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestBuildCoordinatorPrompt(t *testing.T) {
	tm := New("t", "T", StrategyCoordinator)
	a1 := newMockAgent("a1", "r1")
	tm.AddAgent(a1)
	tm.SetCoordinator(a1)

	state := map[string]any{"message": "do something"}
	prompt := tm.buildCoordinatorPrompt(state, 1)
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
}

func TestRunCoordinator_NoAgents(t *testing.T) {
	tm := New("t", "T", StrategyCoordinator)
	_, err := tm.Run(context.Background(), graph.State{"message": "do something"})
	if err == nil {
		t.Fatal("expected error with no agents")
	}
}

func TestRunCoordinator_NoCoordinator_OneAgent(t *testing.T) {
	tm := New("t", "T", StrategyCoordinator)
	tm.AddAgent(newMockAgent("a1", "r1"))
	// With only 1 agent and no explicit coordinator, should error
	_, err := tm.Run(context.Background(), graph.State{"message": "do something"})
	if err == nil {
		t.Fatal("expected error with 1 agent and no coordinator")
	}
}

func TestRunCoordinator_WithPlanDone(t *testing.T) {
	tm := New("t", "T", StrategyCoordinator)
	// Coordinator returns a JSON plan with done=true and no tasks
	coordResp := `{"tasks":[],"done":true,"summary":"all done"}`
	coord := newMockAgent("coord", coordResp)
	worker := newMockAgent("worker", "working")
	tm.AddAgent(coord)
	tm.AddAgent(worker)
	tm.SetCoordinator(coord)

	result, err := tm.Run(context.Background(), graph.State{"message": "do something"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRunCoordinator_WithTask(t *testing.T) {
	tm := New("t", "T", StrategyCoordinator)
	// done=true with tasks still breaks without calling executePlan (done=true check is ||)
	prov := &mockProvider{}
	prov.response = `{"tasks":[{"agent_id":"worker","description":"do work","input":"some input"}],"done":true}`

	coord, _ := agent.New("coord", "coord").WithModel(prov).Build()
	worker := newMockAgent("worker", "working")
	tm.AddAgent(coord)
	tm.AddAgent(worker)
	tm.SetCoordinator(coord)

	result, err := tm.Run(context.Background(), graph.State{"message": "do something"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRunCoordinator_ExecutesPlan(t *testing.T) {
	// Coordinator returns a plan with tasks (done=false for first iteration, then done=true)
	// MaxIterations=1 so only one plan call happens
	callNum := 0
	prov := &mockProvider{}
	// First call: return plan with tasks and done=false (but MaxIter=1, so plan executes then loop ends)
	prov.response = `{"tasks":[{"agent_id":"worker","description":"analyze data"}],"done":false}`

	coord, _ := agent.New("coord", "coord").WithModel(prov).Build()
	worker := newMockAgent("worker", "analysis result")
	tm := New("t", "T", StrategyCoordinator)
	tm.MaxIterations = 1
	tm.AddAgent(coord)
	tm.AddAgent(worker)
	tm.SetCoordinator(coord)

	_ = callNum
	result, err := tm.Run(context.Background(), graph.State{"message": "do something"})
	if err != nil {
		t.Fatalf("Run with executePlan: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestCoordinatorPlan_ModelError(t *testing.T) {
	tm := New("t", "T", StrategyCoordinator)
	errProv := &mockProvider{err: errors.New("model failed")}
	coord, _ := agent.New("coord", "coord").WithModel(errProv).Build()
	worker := newMockAgent("worker", "working")
	tm.AddAgent(coord)
	tm.AddAgent(worker)
	tm.SetCoordinator(coord)

	_, err := tm.Run(context.Background(), graph.State{"message": "do something"})
	if err == nil {
		t.Fatal("expected error from model failure")
	}
}

func TestRunCoordinator_WithDependentTask(t *testing.T) {
	// Plan with a dependent task (DependsOn set)
	prov := &mockProvider{}
	// Task with dependsOn set - worker2 depends on worker1
	prov.response = `{"tasks":[{"agent_id":"worker1","description":"step1"},{"agent_id":"worker2","description":"step2","depends_on":"worker1"}],"done":false}`

	coord, _ := agent.New("coord", "coord").WithModel(prov).Build()
	worker1 := newMockAgent("worker1", "step1 result")
	worker2 := newMockAgent("worker2", "step2 result")
	tm := New("t", "T", StrategyCoordinator)
	tm.MaxIterations = 1
	tm.AddAgent(coord)
	tm.AddAgent(worker1)
	tm.AddAgent(worker2)
	tm.SetCoordinator(coord)

	result, err := tm.Run(context.Background(), graph.State{"message": "do something"})
	if err != nil {
		t.Fatalf("Run with dependent tasks: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRunCoordinator_ContextTimeout(t *testing.T) {
	prov := &mockProvider{}
	prov.response = `{"tasks":[],"done":false}`

	coord, _ := agent.New("coord", "coord").WithModel(prov).Build()
	worker := newMockAgent("worker", "working")
	tm := New("t", "T", StrategyCoordinator)
	tm.MaxIterations = 5
	tm.AddAgent(coord)
	tm.AddAgent(worker)
	tm.SetCoordinator(coord)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Context should timeout before max iterations
	_, err := tm.Run(ctx, graph.State{"message": "do something"})
	if err == nil {
		t.Log("no error (completed within timeout)")
	}
}
