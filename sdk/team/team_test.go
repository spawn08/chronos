package team

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

type mockProvider struct {
	response string
	err      error
}

func (p *mockProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &model.ChatResponse{Content: p.response}, nil
}

func (p *mockProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *mockProvider) Name() string  { return "mock" }
func (p *mockProvider) Model() string { return "mock-model" }

func newMockAgent(id, response string) *agent.Agent {
	a, _ := agent.New(id, id).
		WithModel(&mockProvider{response: response}).
		Build()
	return a
}

func TestNew(t *testing.T) {
	tm := New("t1", "Test Team", StrategySequential)
	if tm.ID != "t1" {
		t.Errorf("ID = %q, want t1", tm.ID)
	}
	if tm.Strategy != StrategySequential {
		t.Errorf("Strategy = %q, want sequential", tm.Strategy)
	}
	if tm.Bus == nil {
		t.Error("Bus should be initialized")
	}
}

func TestSequential(t *testing.T) {
	tm := New("seq", "Sequential", StrategySequential)
	tm.AddAgent(newMockAgent("a1", "result-from-a1"))
	tm.AddAgent(newMockAgent("a2", "result-from-a2"))

	result, err := tm.Run(context.Background(), graph.State{"message": "hello"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	resp, _ := result["response"].(string)
	if resp != "result-from-a2" {
		t.Errorf("response = %q, want result-from-a2 (last agent)", resp)
	}
}

func TestParallel(t *testing.T) {
	tm := New("par", "Parallel", StrategyParallel)
	tm.AddAgent(newMockAgent("a1", "r1"))
	tm.AddAgent(newMockAgent("a2", "r2"))

	result, err := tm.Run(context.Background(), graph.State{"message": "hello"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRouter_WithStaticRouter(t *testing.T) {
	tm := New("rtr", "Router", StrategyRouter)
	tm.AddAgent(newMockAgent("writer", "written content"))
	tm.AddAgent(newMockAgent("coder", "coded solution"))
	tm.SetRouter(func(state graph.State) string {
		if state["type"] == "code" {
			return "coder"
		}
		return "writer"
	})

	result, err := tm.Run(context.Background(), graph.State{"message": "write code", "type": "code"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	resp, _ := result["response"].(string)
	if resp != "coded solution" {
		t.Errorf("response = %q, want coded solution", resp)
	}
}

func TestAddAgent(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	a := newMockAgent("a1", "r")
	tm.AddAgent(a)

	if len(tm.Agents) != 1 {
		t.Errorf("Agents count = %d, want 1", len(tm.Agents))
	}
	if len(tm.Order) != 1 {
		t.Errorf("Order count = %d, want 1", len(tm.Order))
	}
	if tm.Order[0] != "a1" {
		t.Errorf("Order[0] = %q, want a1", tm.Order[0])
	}
}

func TestUnknownStrategy(t *testing.T) {
	tm := New("t", "T", "invalid")
	_, err := tm.Run(context.Background(), graph.State{})
	if err == nil {
		t.Fatal("expected error for unknown strategy")
	}
}

func TestAgentInfoList(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	a1, _ := agent.New("a1", "Agent 1").
		Description("First agent").
		WithModel(&mockProvider{response: "r"}).
		Build()
	tm.AddAgent(a1)

	infos := tm.agentInfoList()
	if len(infos) != 1 {
		t.Fatalf("expected 1 info, got %d", len(infos))
	}
	if infos[0].ID != "a1" {
		t.Errorf("ID = %q", infos[0].ID)
	}
	if infos[0].Name != "Agent 1" {
		t.Errorf("Name = %q", infos[0].Name)
	}
}

func TestMessageHistory(t *testing.T) {
	tm := New("t", "T", StrategySequential)
	tm.AddAgent(newMockAgent("a1", "r1"))
	tm.AddAgent(newMockAgent("a2", "r2"))

	_, _ = tm.Run(context.Background(), graph.State{"message": "test"})

	history := tm.MessageHistory()
	if history == nil {
		t.Error("expected message history")
	}
}
