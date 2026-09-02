package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
)

func TestExecuteToolCallsRunsParallelSafeToolsConcurrently(t *testing.T) {
	a, err := New("parallel", "Parallel").WithModel(&recordingProvider{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 2)
	release := make(chan struct{})
	for _, name := range []string{"one", "two"} {
		toolName := name
		a.Tools.Register(&tool.Definition{
			Name:         toolName,
			Permission:   tool.PermAllow,
			ParallelSafe: true,
			Handler: func(context.Context, map[string]any) (any, error) {
				started <- toolName
				<-release
				return toolName, nil
			},
		})
	}
	done := make(chan error, 1)
	go func() {
		_, err := a.executeToolCalls(context.Background(), nil, &model.ChatResponse{ToolCalls: []model.ToolCall{
			{ID: "1", Name: "one", Arguments: `{}`},
			{ID: "2", Name: "two", Arguments: `{}`},
		}})
		done <- err
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("parallel-safe tool calls did not start concurrently")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("executeToolCalls() error = %v", err)
	}
}

// recordingProvider returns a scripted sequence of responses and records a
// snapshot of every request it receives, so tests can assert that message
// history accumulates across tool-calling rounds and that tool definitions are
// forwarded on each follow-up call.
type recordingProvider struct {
	mu      sync.Mutex
	replies []*model.ChatResponse
	idx     int

	// recorded per-call snapshots
	msgLens   []int
	toolCount []int
	roleSeqs  [][]string
}

func (p *recordingProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	roles := make([]string, len(req.Messages))
	for i := range req.Messages {
		roles[i] = req.Messages[i].Role
	}
	p.msgLens = append(p.msgLens, len(req.Messages))
	p.toolCount = append(p.toolCount, len(req.Tools))
	p.roleSeqs = append(p.roleSeqs, roles)

	if p.idx >= len(p.replies) {
		return p.replies[len(p.replies)-1], nil
	}
	r := p.replies[p.idx]
	p.idx++
	return r, nil
}

func (p *recordingProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}
func (p *recordingProvider) Name() string  { return "recording" }
func (p *recordingProvider) Model() string { return "recording-model" }

func toolCallResp(id, name string) *model.ChatResponse {
	return &model.ChatResponse{
		StopReason: model.StopReasonToolCall,
		ToolCalls:  []model.ToolCall{{ID: id, Name: name, Arguments: "{}"}},
	}
}

// TestChat_MultiRoundToolContext verifies that across multiple tool-calling
// rounds the message history accumulates (prior assistant tool-call messages
// and tool results are retained) and the tool definitions are passed on every
// follow-up model call.
func TestChat_MultiRoundToolContext(t *testing.T) {
	p := &recordingProvider{
		replies: []*model.ChatResponse{
			toolCallResp("t1", "search"),                               // round 1
			toolCallResp("t2", "search"),                               // round 2
			{StopReason: model.StopReasonEnd, Content: "final answer"}, // done
		},
	}

	a, _ := New("a1", "T").WithModel(p).Build()
	a.Tools.Register(&tool.Definition{
		Name:       "search",
		Permission: tool.PermAllow,
		Parameters: map[string]any{"type": "object"},
		Handler: func(context.Context, map[string]any) (any, error) {
			return "result", nil
		},
	})

	resp, err := a.Chat(context.Background(), "go")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "final answer" {
		t.Fatalf("content = %q, want %q", resp.Content, "final answer")
	}

	// Exactly three model calls: initial + two follow-ups.
	if len(p.msgLens) != 3 {
		t.Fatalf("expected 3 model calls, got %d (msgLens=%v)", len(p.msgLens), p.msgLens)
	}

	// Message history must strictly grow as tool rounds accumulate. Each round
	// adds one assistant tool-call message and one tool result message.
	tests := []struct {
		call         int
		wantMsgLen   int
		wantTools    bool
		wantLastRole string
	}{
		{call: 0, wantMsgLen: 1, wantTools: true, wantLastRole: model.RoleUser}, // [user]
		{call: 1, wantMsgLen: 3, wantTools: true, wantLastRole: model.RoleTool}, // + assistant + tool
		{call: 2, wantMsgLen: 5, wantTools: true, wantLastRole: model.RoleTool}, // + assistant + tool
	}
	for _, tt := range tests {
		if got := p.msgLens[tt.call]; got != tt.wantMsgLen {
			t.Errorf("call %d: message count = %d, want %d (roles=%v)", tt.call, got, tt.wantMsgLen, p.roleSeqs[tt.call])
		}
		if tt.wantTools && p.toolCount[tt.call] == 0 {
			t.Errorf("call %d: tools were not passed on the model call", tt.call)
		}
		roles := p.roleSeqs[tt.call]
		if len(roles) == 0 || roles[len(roles)-1] != tt.wantLastRole {
			t.Errorf("call %d: last role = %v, want %v", tt.call, roles, tt.wantLastRole)
		}
	}

	// The final follow-up must still contain the first round's assistant and
	// tool messages — proof that context is not dropped between rounds.
	finalRoles := p.roleSeqs[2]
	assistantCount, toolCount := 0, 0
	for _, r := range finalRoles {
		switch r {
		case model.RoleAssistant:
			assistantCount++
		case model.RoleTool:
			toolCount++
		}
	}
	if assistantCount != 2 || toolCount != 2 {
		t.Errorf("final call role composition = %v; want 2 assistant + 2 tool messages", finalRoles)
	}
}

// TestChat_MaxIterationsExceededError verifies that when the iteration limit
// is exceeded while tool calls remain unsatisfied, Chat returns a clear error
// (not a partial response).
func TestChat_MaxIterationsExceededError(t *testing.T) {
	tests := []struct {
		name    string
		maxIter int
	}{
		{name: "limit-1", maxIter: 1},
		{name: "limit-3", maxIter: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Always returns a tool call, so the loop can never satisfy it.
			p := &recordingProvider{replies: []*model.ChatResponse{toolCallResp("loop", "spin")}}
			a, _ := New("a1", "T").WithModel(p).WithMaxIterations(tt.maxIter).Build()
			a.Tools.Register(&tool.Definition{
				Name:       "spin",
				Permission: tool.PermAllow,
				Parameters: map[string]any{"type": "object"},
				Handler:    func(context.Context, map[string]any) (any, error) { return "n", nil },
			})

			resp, err := a.Chat(context.Background(), "go")
			if err == nil {
				t.Fatalf("expected error, got resp=%+v", resp)
				return
			}
			if resp != nil {
				t.Fatalf("expected nil response on error, got %+v", resp)
			}
		})
	}
}
