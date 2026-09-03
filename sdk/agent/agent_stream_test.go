package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	chronostrace "github.com/spawn08/chronos/os/trace"
)

// streamProvider is a mock Provider that emits a scripted sequence of streaming
// responses. Each call to StreamChat pops the next script entry.
type streamProvider struct {
	scripts  [][]*model.ChatResponse // one slice of deltas per model call
	startErr error
	calls    int
	requests []*model.ChatRequest
}

func (p *streamProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	return nil, errors.New("not used")
}

func (p *streamProvider) StreamChat(_ context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	if p.startErr != nil {
		return nil, p.startErr
	}
	idx := p.calls
	p.calls++
	p.requests = append(p.requests, req)
	ch := make(chan *model.ChatResponse, 8)
	go func() {
		defer close(ch)
		if idx < len(p.scripts) {
			for _, cr := range p.scripts[idx] {
				ch <- cr
			}
		}
	}()
	return ch, nil
}

func (p *streamProvider) Name() string  { return "stream-mock" }
func (p *streamProvider) Model() string { return "stream-mock-model" }

// collectStream drains a ChatStream channel, returning the concatenated delta
// text, the final aggregated usage and any terminal error.
func collectStream(t *testing.T, ch <-chan *model.ChatResponse) (string, model.Usage, error) {
	t.Helper()
	var b strings.Builder
	var usage model.Usage
	for chunk := range ch {
		if chunk.Err != nil {
			return b.String(), usage, chunk.Err
		}
		if chunk.Delta {
			b.WriteString(chunk.Content)
			continue
		}
		usage = chunk.Usage
	}
	return b.String(), usage, nil
}

func TestChatStream_TextDeltas(t *testing.T) {
	prov := &streamProvider{scripts: [][]*model.ChatResponse{{
		{Role: model.RoleAssistant, Content: "Hel", Delta: true},
		{Role: model.RoleAssistant, Content: "lo ", Delta: true},
		{Role: model.RoleAssistant, Content: "world", Delta: true},
		{Role: model.RoleAssistant, Usage: model.Usage{PromptTokens: 3, CompletionTokens: 5}, StopReason: model.StopReasonEnd},
	}}}
	a, _ := New("a1", "Test").WithModel(prov).Build()

	ch, err := a.ChatStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("ChatStream returned error: %v", err)
	}
	text, usage, streamErr := collectStream(t, ch)
	if streamErr != nil {
		t.Fatalf("stream error: %v", streamErr)
	}
	if text != "Hello world" {
		t.Errorf("text = %q, want %q", text, "Hello world")
	}
	if usage.PromptTokens != 3 || usage.CompletionTokens != 5 {
		t.Errorf("usage = %+v, want {3 5}", usage)
	}
}

func TestChatStream_EmitsHooksAndTrace(t *testing.T) {
	prov := &streamProvider{scripts: [][]*model.ChatResponse{{
		{Role: model.RoleAssistant, Content: "ok", Delta: true},
		{Role: model.RoleAssistant, StopReason: model.StopReasonEnd},
	}}}
	store := newTestStorage()
	tracer := chronostrace.NewCollector(store)
	logger := &hooks.LoggingHook{}
	a, _ := New("observed", "Observed").WithModel(prov).WithTracer(tracer).AddHook(logger).Build()

	ch, err := a.ChatStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if _, _, streamErr := collectStream(t, ch); streamErr != nil {
		t.Fatalf("stream error: %v", streamErr)
	}

	var before, after int
	for _, event := range logger.Events {
		switch event.Type {
		case hooks.EventModelCallBefore:
			before++
		case hooks.EventModelCallAfter:
			after++
		}
	}
	if before != 1 || after != 1 {
		t.Fatalf("model hook counts before=%d after=%d, want 1/1", before, after)
	}
	if len(store.traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(store.traces))
	}
	for _, span := range store.traces {
		if span.Kind != "model_call" || span.EndedAt.IsZero() {
			t.Fatalf("incomplete model span: %#v", span)
		}
	}
}

func TestChatStream_StartError(t *testing.T) {
	prov := &streamProvider{startErr: errors.New("connect failed")}
	a, _ := New("a1", "Test").WithModel(prov).Build()

	ch, err := a.ChatStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("ChatStream returned error: %v", err)
	}
	_, _, streamErr := collectStream(t, ch)
	if streamErr == nil || !strings.Contains(streamErr.Error(), "connect failed") {
		t.Errorf("expected connect error on stream, got %v", streamErr)
	}
}

func TestChatStream_NoModel(t *testing.T) {
	a, _ := New("a1", "Test").Build()
	if _, err := a.ChatStream(context.Background(), "hi"); err == nil {
		t.Fatal("expected error when agent has no model")
	}
}

func TestChatStream_ToolCallThenStream(t *testing.T) {
	// First round: model requests a tool. Second round: streams the answer.
	prov := &streamProvider{scripts: [][]*model.ChatResponse{
		{
			{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "1", Name: "echo", Arguments: `{"v":"x"}`}}, Usage: model.Usage{PromptTokens: 10, CompletionTokens: 1}, StopReason: model.StopReasonToolCall},
		},
		{
			{Role: model.RoleAssistant, Content: "done: ", Delta: true},
			{Role: model.RoleAssistant, Content: "x", Delta: true},
			{Role: model.RoleAssistant, Usage: model.Usage{PromptTokens: 4, CompletionTokens: 2}, StopReason: model.StopReasonEnd},
		},
	}}

	var called bool
	echo := &tool.Definition{
		Name:        "echo",
		Description: "echo",
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			called = true
			return args["v"], nil
		},
	}
	a, _ := New("a1", "Test").WithModel(prov).AddTool(echo).Build()

	ch, err := a.ChatStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("ChatStream returned error: %v", err)
	}
	var text strings.Builder
	var streamedTools []model.ToolCall
	var usage model.Usage
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		text.WriteString(chunk.Content)
		streamedTools = append(streamedTools, chunk.ToolCalls...)
		if chunk.Usage.PromptTokens > 0 {
			usage = chunk.Usage
		}
	}
	if !called {
		t.Error("tool was not executed")
	}
	if text.String() != "done: x" {
		t.Errorf("text = %q, want %q", text.String(), "done: x")
	}
	if prov.calls != 2 {
		t.Errorf("expected 2 model calls (tool round + answer), got %d", prov.calls)
	}
	if len(streamedTools) != 1 || streamedTools[0].Name != "echo" || streamedTools[0].Arguments != `{"v":"x"}` {
		t.Errorf("streamed tool calls = %+v, want completed echo call", streamedTools)
	}
	if usage.PromptTokens != 14 || usage.CompletionTokens != 3 || usage.ContextTokens != 6 {
		t.Errorf("usage = %+v, want aggregate 14/3 and final-call context 6", usage)
	}
}

func TestChatStreamWithSessionPreservesConversation(t *testing.T) {
	prov := &streamProvider{scripts: [][]*model.ChatResponse{
		{
			{Role: model.RoleAssistant, Content: "Paris", Delta: true},
			{Role: model.RoleAssistant, Usage: model.Usage{PromptTokens: 4, CompletionTokens: 1}, StopReason: model.StopReasonEnd},
		},
		{
			{Role: model.RoleAssistant, Content: "France", Delta: true},
			{Role: model.RoleAssistant, Usage: model.Usage{PromptTokens: 8, CompletionTokens: 1}, StopReason: model.StopReasonEnd},
		},
	}}
	a, _ := New("a1", "Test").WithModel(prov).WithStorage(newTestStorage()).Build()

	first, err := a.ChatStreamWithSession(context.Background(), "session-1", "Remember Paris")
	if err != nil {
		t.Fatalf("first ChatStreamWithSession: %v", err)
	}
	if text, _, streamErr := collectStream(t, first); streamErr != nil || text != "Paris" {
		t.Fatalf("first stream = %q, %v; want Paris", text, streamErr)
	}

	second, err := a.ChatStreamWithSession(context.Background(), "session-1", "Which country?")
	if err != nil {
		t.Fatalf("second ChatStreamWithSession: %v", err)
	}
	if _, _, streamErr := collectStream(t, second); streamErr != nil {
		t.Fatalf("second stream: %v", streamErr)
	}

	if len(prov.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(prov.requests))
	}
	var conversation []string
	for _, msg := range prov.requests[1].Messages {
		if msg.Role == model.RoleUser || msg.Role == model.RoleAssistant {
			conversation = append(conversation, msg.Role+":"+msg.Content)
		}
	}
	want := []string{"user:Remember Paris", "assistant:Paris", "user:Which country?"}
	if strings.Join(conversation, "|") != strings.Join(want, "|") {
		t.Fatalf("second request conversation = %q, want %q", conversation, want)
	}
}
