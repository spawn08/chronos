package team

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

type lockedCheckpointProvider struct {
	mu       sync.Mutex
	response string
	calls    int
	identity agent.RunIdentity
	fail     error
}

func (p *lockedCheckpointProvider) Chat(ctx context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.identity, _ = agent.RunIdentityFromContext(ctx)
	if p.fail != nil {
		return nil, p.fail
	}
	return &model.ChatResponse{Content: p.response}, nil
}
func (p *lockedCheckpointProvider) StreamChat(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("streaming not used")
}
func (p *lockedCheckpointProvider) Name() string  { return "fixture" }
func (p *lockedCheckpointProvider) Model() string { return "fixture" }

func (p *lockedCheckpointProvider) snapshot() (int, agent.RunIdentity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, p.identity
}

func TestParallelCheckpointResumesOnlyUncheckpointedMembers(t *testing.T) {
	providers := []*lockedCheckpointProvider{{response: "alpha"}, {response: "beta"}, {response: "gamma"}}
	create := func() *Team {
		team := New("par", "Team", StrategyParallel)
		for i, id := range []string{"a", "b", "c"} {
			built, err := agent.New(id, id).WithModel(providers[i]).Build()
			if err != nil {
				t.Fatal(err)
			}
			team.AddAgent(built)
		}
		return team
	}
	ctx := agent.WithRunIdentity(context.Background(), agent.RunIdentity{DeliveryID: "delivery", TaskID: "delivery", RoleID: "worker", InvocationID: "owner-1"})
	completed := map[int]string{}
	crash := errors.New("worker stopped at member b")
	_, err := create().RunParallelWithCheckpoints(ctx, graph.State{"message": "inspect"}, nil, func(_ context.Context, step int, member, response string) error {
		if member == "b" {
			return crash
		}
		completed[step] = response
		return nil
	})
	if !errors.Is(err, crash) {
		t.Fatalf("interrupted parallel team error = %v", err)
	}
	if _, ok := completed[1]; ok {
		t.Fatalf("failed checkpoint recorded as a receipt: %v", completed)
	}
	before := make([]int, len(providers))
	for i, p := range providers {
		before[i], _ = p.snapshot()
		if before[i] > 1 || (completed[i] != "" && before[i] != 1) {
			t.Fatalf("calls before resume = %v, receipts = %v", before, completed)
		}
	}
	checkpointed := map[int]bool{}
	for step := range completed {
		checkpointed[step] = true
	}

	ctx = agent.WithRunIdentity(context.Background(), agent.RunIdentity{DeliveryID: "delivery", TaskID: "delivery", RoleID: "worker", InvocationID: "owner-2"})
	result, err := create().RunParallelWithCheckpoints(ctx, graph.State{"message": "inspect"}, completed, func(_ context.Context, step int, member, response string) error {
		// Writing into the caller's own map while members run must be safe.
		completed[step] = response
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"a", "b", "c"} {
		calls, identity := providers[i].snapshot()
		if checkpointed[i] {
			if calls != before[i] {
				t.Fatalf("checkpointed member %s resubmitted: %d -> %d", id, before[i], calls)
			}
			continue
		}
		if calls != before[i]+1 || identity.NodeID != fmt.Sprintf("team:par:%d", i) || identity.ParentInvocationID != "owner-2" || identity.RoleID != id {
			t.Fatalf("resumed member %s identity = %+v, calls = %d", id, identity, calls)
		}
	}
	got, _ := result["response"].(string)
	if len(completed) != 3 || strings.Index(got, "alpha") < 0 || strings.Index(got, "alpha") > strings.Index(got, "beta") || strings.Index(got, "beta") > strings.Index(got, "gamma") {
		t.Fatalf("resumed result = %q, completed = %v", got, completed)
	}
}

func TestParallelCheckpointRejectsInvalidRequests(t *testing.T) {
	provider := &lockedCheckpointProvider{response: "x"}
	a, err := agent.New("a", "a").WithModel(provider).Build()
	if err != nil {
		t.Fatal(err)
	}
	team := New("par", "Team", StrategyParallel).AddAgent(a)
	noop := func(context.Context, int, string, string) error { return nil }
	if _, err := team.RunParallelWithCheckpoints(context.Background(), graph.State{}, map[int]string{1: "x"}, noop); err == nil {
		t.Fatal("out-of-range completed member accepted")
	}
	if _, err := team.RunParallelWithCheckpoints(context.Background(), graph.State{}, nil, nil); err == nil {
		t.Fatal("nil checkpoint accepted")
	}
	if _, err := New("seq", "Team", StrategySequential).AddAgent(a).RunParallelWithCheckpoints(context.Background(), graph.State{}, nil, noop); err == nil {
		t.Fatal("sequential team accepted parallel checkpoints")
	}
	provider.fail = errors.New("provider down")
	if _, err := team.RunParallelWithCheckpoints(context.Background(), graph.State{}, nil, func(context.Context, int, string, string) error {
		t.Fatal("failed member was checkpointed")
		return nil
	}); err == nil {
		t.Fatal("failed member produced success")
	}
}
