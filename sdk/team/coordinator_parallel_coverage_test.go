package team

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

// --- Providers for parallel / coordinator edge cases ---

type sleepThenOKProvider struct {
	d time.Duration
}

func (p *sleepThenOKProvider) Chat(ctx context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	select {
	case <-time.After(p.d):
		return &model.ChatResponse{Content: "fast-done"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *sleepThenOKProvider) StreamChat(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *sleepThenOKProvider) Name() string  { return "sleep" }
func (p *sleepThenOKProvider) Model() string { return "sleep-model" }

type waitCtxDoneProvider struct{}

func (waitCtxDoneProvider) Chat(ctx context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (waitCtxDoneProvider) StreamChat(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (waitCtxDoneProvider) Name() string  { return "wait" }
func (waitCtxDoneProvider) Model() string { return "wait-model" }

func agentWithProvider(id string, p model.Provider) *agent.Agent {
	a, _ := agent.New(id, id).WithModel(p).Build()
	return a
}

func TestNewSwarm_ZeroAgents_Table(t *testing.T) {
	tests := []struct {
		name    string
		agents  []*agent.Agent
		wantSub string
	}{
		{"nil_slice", nil, "at least 2 agents"},
		{"empty_slice", []*agent.Agent{}, "at least 2 agents"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSwarm(SwarmConfig{Agents: tt.agents})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("err=%q want substring %q", err.Error(), tt.wantSub)
			}
		})
	}
}

// MaxConcurrency=1: one agent holds the semaphore for a sleep; the other blocks on sem acquisition.
// Cancelling the context while the waiter is blocked exercises runParallel's select on ctx.Done() before sem.
func TestRunParallel_ContextCancelWhileWaitingOnSemaphore(t *testing.T) {
	tm := New("sem-wait", "Sem", StrategyParallel)
	tm.SetMaxConcurrency(1)
	tm.SetErrorStrategy(ErrorStrategyFailFast)
	// Order matters for readability: first agent tends to win the race first; it sleeps holding the slot.
	tm.AddAgent(agentWithProvider("holds", &sleepThenOKProvider{d: 200 * time.Millisecond}))
	tm.AddAgent(agentWithProvider("waits", waitCtxDoneProvider{}))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()

	_, err := tm.Run(ctx, graph.State{"message": "x"})
	if err == nil {
		t.Fatal("expected error after cancellation / agent failure")
	}
}

func TestRunParallel_BestEffort_OneAgentFails(t *testing.T) {
	tm := New("be", "BE", StrategyParallel)
	tm.SetErrorStrategy(ErrorStrategyBestEffort)
	tm.AddAgent(newMockAgentWithError("bad", errors.New("nope")))
	tm.AddAgent(newMockAgent("good", "ok-response"))

	out, err := tm.Run(context.Background(), graph.State{"message": "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, _ := out["response"].(string)
	if !strings.Contains(resp, "ok-response") {
		t.Fatalf("expected successful agent output merged, got %q", resp)
	}
}

func TestRunSequential_ContextCancelAfterFirstAgent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	prov := &cancelOnFirstChatProvider{cancel: cancel, response: "step1"}
	a1, _ := agent.New("a1", "a1").WithModel(prov).Build()
	a2 := newMockAgent("a2", "step2")

	tm := New("seq-mid", "S", StrategySequential)
	tm.AddAgent(a1)
	tm.AddAgent(a2)

	_, err := tm.Run(ctx, graph.State{"message": "go"})
	if err == nil {
		t.Fatal("expected cancellation before second agent")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// cancelOnFirstChatProvider cancels the run context during the first model call (first sequential step).
type cancelOnFirstChatProvider struct {
	cancel   context.CancelFunc
	response string
}

func (p *cancelOnFirstChatProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	p.cancel()
	return &model.ChatResponse{Content: p.response}, nil
}

func (p *cancelOnFirstChatProvider) StreamChat(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *cancelOnFirstChatProvider) Name() string  { return "c1" }
func (p *cancelOnFirstChatProvider) Model() string { return "c1" }

func TestCoordinatorPlan_JSONInMarkdownFences(t *testing.T) {
	raw := "```json\n{\"tasks\":[{\"agent_id\":\"w\",\"description\":\"do work\"}],\"done\":false}\n```"
	tm := New("md", "MD", StrategyCoordinator)
	prov := &mockProvider{response: raw}
	coord, _ := agent.New("coord", "coord").WithModel(prov).Build()
	w := newMockAgent("w", "done")
	tm.AddAgent(coord)
	tm.AddAgent(w)
	tm.SetCoordinator(coord)

	_, err := tm.Run(context.Background(), graph.State{"message": "task"})
	if err != nil {
		t.Fatalf("expected markdown-wrapped JSON to parse: %v", err)
	}
}

func TestRunCoordinator_EmptyTaskListExitsWithoutDelegate(t *testing.T) {
	tm := New("empty-plan", "E", StrategyCoordinator)
	prov := &mockProvider{response: `{"tasks":[],"done":false}`}
	coord, _ := agent.New("coord", "coord").WithModel(prov).Build()
	w := newMockAgent("w", "unused")
	tm.AddAgent(coord)
	tm.AddAgent(w)
	tm.SetCoordinator(coord)

	out, err := tm.Run(context.Background(), graph.State{"message": "noop", "input": "x"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == nil {
		t.Fatal("nil state")
	}
}

func TestExecutePlan_DependencyNotCompleted(t *testing.T) {
	tm := New("dep-bad", "D", StrategyCoordinator)
	// Independent task for w, then task for w2 that depends on missing agent "ghost".
	prov := &mockProvider{response: `{"tasks":[
		{"agent_id":"w","description":"first"},
		{"agent_id":"w2","description":"second","depends_on":"ghost"}
	],"done":false}`}
	coord, _ := agent.New("coord", "coord").WithModel(prov).Build()
	tm.AddAgent(coord)
	tm.AddAgent(newMockAgent("w", "a"))
	tm.AddAgent(newMockAgent("w2", "b"))
	tm.SetCoordinator(coord)

	_, err := tm.Run(context.Background(), graph.State{"message": "x"})
	if err == nil {
		t.Fatal("expected execute plan error for missing dependency")
	}
	if !strings.Contains(err.Error(), "ghost") || !strings.Contains(err.Error(), "not completed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetCoordinator_NilPanics(t *testing.T) {
	tm := New("nil-coord", "N", StrategyCoordinator)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when SetCoordinator(nil) calls Register on nil agent")
		}
	}()
	tm.SetCoordinator(nil)
}

func TestNewHierarchy_LeafSupervisorOnly(t *testing.T) {
	sup := newMockAgent("root", "root-out")
	root := &SupervisorNode{
		Supervisor: sup,
		Workers:    nil,
		SubTeams:   nil,
	}
	tm, err := NewHierarchy(HierarchyConfig{Root: root})
	if err != nil {
		t.Fatalf("NewHierarchy: %v", err)
	}
	if tm.Strategy != "hierarchy" {
		t.Fatalf("strategy=%q", tm.Strategy)
	}
}
