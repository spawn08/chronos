package team

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

type checkpointProvider struct {
	response string
	calls    int
	identity agent.RunIdentity
}

func (p *checkpointProvider) Chat(ctx context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	p.calls++
	p.identity, _ = agent.RunIdentityFromContext(ctx)
	return &model.ChatResponse{Content: p.response}, nil
}
func (p *checkpointProvider) StreamChat(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("streaming not used")
}
func (p *checkpointProvider) Name() string  { return "fixture" }
func (p *checkpointProvider) Model() string { return "fixture" }

func TestSequentialCheckpointResumesOnlyIncompleteMember(t *testing.T) {
	first, second := &checkpointProvider{response: "first result"}, &checkpointProvider{response: "second result"}
	a, err := agent.New("first", "First").WithModel(first).Build()
	if err != nil {
		t.Fatal(err)
	}
	b, err := agent.New("second", "Second").WithModel(second).Build()
	if err != nil {
		t.Fatal(err)
	}
	parent := agent.RunIdentity{TaskID: "delivery", DeliveryID: "delivery", RoleID: "worker", InvocationID: "owner-1"}
	ctx := agent.WithRunIdentity(context.Background(), parent)
	create := func() *Team { return New("seq", "Team", StrategySequential).AddAgent(a).AddAgent(b) }
	var completed []string
	crash := errors.New("worker stopped after first receipt")
	_, err = create().RunSequentialWithCheckpoints(ctx, graph.State{"messages": "inspect"}, nil, func(_ context.Context, step int, member, response string) error {
		if step != 0 || member != "first" {
			t.Fatalf("unexpected first checkpoint: %d %q", step, member)
		}
		completed = append(completed, response)
		return crash
	})
	if !errors.Is(err, crash) || first.calls != 1 || second.calls != 0 {
		t.Fatalf("interrupted team = %v; calls first=%d second=%d", err, first.calls, second.calls)
	}
	ctx = agent.WithRunIdentity(context.Background(), agent.RunIdentity{TaskID: "delivery", DeliveryID: "delivery", RoleID: "worker", InvocationID: "owner-2"})
	result, err := create().RunSequentialWithCheckpoints(ctx, graph.State{"messages": "inspect"}, completed, func(_ context.Context, step int, member, response string) error {
		if step != 1 || member != "second" || response != "second result" {
			t.Fatalf("resumed checkpoint: %d %q %q", step, member, response)
		}
		return nil
	})
	if err != nil || result["response"] != "second result" || first.calls != 1 || second.calls != 1 {
		t.Fatalf("resumed team result=%v err=%v calls first=%d second=%d", result, err, first.calls, second.calls)
	}
	if second.identity.DeliveryID != "delivery" || second.identity.ParentInvocationID != "owner-2" || second.identity.RoleID != "second" || second.identity.NodeID != "team:seq:1" {
		t.Fatalf("resumed member identity = %+v", second.identity)
	}
}
