package team

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

// streamingProvider emits its response one rune-chunk at a time so tests can
// verify token-level streaming.
type streamingProvider struct {
	response  string
	reasoning string
}

func (p *streamingProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	return &model.ChatResponse{Content: p.response, Role: model.RoleAssistant, StopReason: model.StopReasonEnd}, nil
}

func (p *streamingProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, len(p.response)+2)
	if p.reasoning != "" {
		ch <- &model.ChatResponse{Role: model.RoleAssistant, Reasoning: p.reasoning, Delta: true}
	}
	// Split the response into a few fragments to exercise reassembly.
	for _, frag := range chunkString(p.response, 3) {
		ch <- &model.ChatResponse{Role: model.RoleAssistant, Content: frag, Delta: true}
	}
	ch <- &model.ChatResponse{Role: model.RoleAssistant, Usage: model.Usage{PromptTokens: 1, CompletionTokens: 2}, StopReason: model.StopReasonEnd}
	close(ch)
	return ch, nil
}

func (p *streamingProvider) Name() string  { return "stream-mock" }
func (p *streamingProvider) Model() string { return "stream-mock-model" }

func chunkString(s string, n int) []string {
	var out []string
	for i := 0; i < len(s); i += n {
		end := i + n
		if end > len(s) {
			end = len(s)
		}
		out = append(out, s[i:end])
	}
	return out
}

func newStreamingAgent(id, response string) *agent.Agent {
	a, _ := agent.New(id, id).WithModel(&streamingProvider{response: response}).Build()
	return a
}

// drain collects all events, returning per-agent concatenated tokens, the final
// state, and any error event.
func drainTeamStream(t *testing.T, ch <-chan TeamStreamEvent) (map[string]string, graph.State, error) {
	t.Helper()
	tokens := make(map[string]string)
	var final graph.State
	var runErr error
	for evt := range ch {
		switch evt.Type {
		case TeamEventToken:
			tokens[evt.AgentID] += evt.Content
		case TeamEventComplete:
			final = evt.State
		case TeamEventError:
			runErr = evt.Err
		}
	}
	return tokens, final, runErr
}

func TestRunStream_Sequential(t *testing.T) {
	tm := New("seq", "Sequential", StrategySequential)
	tm.AddAgent(newStreamingAgent("a1", "hello"))
	tm.AddAgent(newStreamingAgent("a2", "world"))

	ch, err := tm.RunStream(context.Background(), graph.State{"message": "hi"})
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	tokens, final, runErr := drainTeamStream(t, ch)
	if runErr != nil {
		t.Fatalf("stream error: %v", runErr)
	}
	if tokens["a1"] != "hello" {
		t.Errorf("a1 tokens = %q, want %q", tokens["a1"], "hello")
	}
	if tokens["a2"] != "world" {
		t.Errorf("a2 tokens = %q, want %q", tokens["a2"], "world")
	}
	if final == nil {
		t.Fatal("expected a final complete state")
	}
	if resp, _ := final["response"].(string); resp != "world" {
		t.Errorf("final response = %q, want %q", resp, "world")
	}
}

func TestRunStream_Parallel(t *testing.T) {
	tm := New("par", "Parallel", StrategyParallel)
	tm.AddAgent(newStreamingAgent("a1", "aaa"))
	tm.AddAgent(newStreamingAgent("a2", "bbb"))

	ch, err := tm.RunStream(context.Background(), graph.State{"message": "hi"})
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	tokens, final, runErr := drainTeamStream(t, ch)
	if runErr != nil {
		t.Fatalf("stream error: %v", runErr)
	}
	if tokens["a1"] != "aaa" || tokens["a2"] != "bbb" {
		t.Errorf("tokens = %v, want a1=aaa a2=bbb", tokens)
	}
	if final == nil {
		t.Fatal("expected a final complete state")
	}
	resp, _ := final["response"].(string)
	if !strings.Contains(resp, "aaa") || !strings.Contains(resp, "bbb") {
		t.Errorf("merged response = %q, want both outputs", resp)
	}
}

func TestRunStream_ForwardsReasoning(t *testing.T) {
	a, _ := agent.New("reasoner", "reasoner").
		WithModel(&streamingProvider{response: "answer", reasoning: "checked options"}).
		WithReasoningConfig(model.ReasoningConfig{Enabled: true, Summary: true}).
		Build()
	tm := New("seq", "Sequential", StrategySequential).AddAgent(a)

	ch, err := tm.RunStream(context.Background(), graph.State{"message": "hi"})
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	var got string
	for evt := range ch {
		if evt.Type == TeamEventReasoning {
			got += evt.Content
		}
	}
	if got != "checked options" {
		t.Fatalf("reasoning = %q", got)
	}
}

type streamFailureProvider struct{ streamingProvider }

func (p *streamFailureProvider) StreamChat(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("stream unavailable")
}

func TestRunStream_DoesNotSilentlyFallbackToBlocking(t *testing.T) {
	a, _ := agent.New("a", "a").WithModel(&streamFailureProvider{streamingProvider{response: "blocking result"}}).Build()
	tm := New("seq", "Sequential", StrategySequential).AddAgent(a)
	ch, err := tm.RunStream(context.Background(), graph.State{"message": "hi"})
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	_, _, runErr := drainTeamStream(t, ch)
	if runErr == nil || !strings.Contains(runErr.Error(), "stream unavailable") {
		t.Fatalf("stream error = %v", runErr)
	}
}

func TestRunStream_BlockingUnaffected(t *testing.T) {
	// Without RunStream, a non-streaming provider (StreamChat unimplemented) must
	// still work via the blocking Chat fallback.
	tm := New("seq", "Sequential", StrategySequential)
	tm.AddAgent(newMockAgent("a1", "result"))

	result, err := tm.Run(context.Background(), graph.State{"message": "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp, _ := result["response"].(string); resp != "result" {
		t.Errorf("response = %q, want %q", resp, "result")
	}
}
