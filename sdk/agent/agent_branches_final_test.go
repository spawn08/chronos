package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/guardrails"
	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/mcp"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/engine/tool"
	chronostrace "github.com/spawn08/chronos/os/trace"
	"github.com/spawn08/chronos/storage"
)

// seqTestProvider returns a sequence of Chat responses for multi-step flows.
type seqTestProvider struct {
	mu      sync.Mutex
	replies []struct {
		resp *model.ChatResponse
		err  error
	}
	i int
}

func (s *seqTestProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.i >= len(s.replies) {
		return &model.ChatResponse{Content: "default", StopReason: model.StopReasonEnd}, nil
	}
	r := s.replies[s.i]
	s.i++
	if r.err != nil {
		return nil, r.err
	}
	return r.resp, nil
}

func (s *seqTestProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *seqTestProvider) Name() string  { return "seq" }
func (s *seqTestProvider) Model() string { return "seq-model" }

type failAfterNAppendStorage struct {
	*testStorage
	okLeft int
}

func (f *failAfterNAppendStorage) AppendEvent(ctx context.Context, e *storage.Event) error {
	if f.okLeft <= 0 {
		return errors.New("append quota exceeded")
	}
	f.okLeft--
	return f.testStorage.AppendEvent(ctx, e)
}

type modelRetryHook struct{}

func (modelRetryHook) Before(context.Context, *hooks.Event) error { return nil }

func (modelRetryHook) After(_ context.Context, evt *hooks.Event) error {
	if evt.Type == hooks.EventModelCallAfter && evt.Error != nil {
		evt.Output = &model.ChatResponse{Content: "recovered-by-hook", StopReason: model.StopReasonEnd}
		evt.Error = nil
	}
	return nil
}

func TestChatWithSession_TriggersSummarizationThenReplies(t *testing.T) {
	store := newTestStorage()
	sid := "sum-flow-sess"
	long := strings.Repeat("word ", 40)
	store.sessions[sid] = &storage.Session{ID: sid, AgentID: "a1", Status: "active"}
	store.events[sid] = []*storage.Event{
		{ID: "e1", SessionID: sid, SeqNum: 1, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "hi"}},
		{ID: "e2", SessionID: sid, SeqNum: 2, Type: "chat_message", Payload: map[string]any{"role": "assistant", "content": "hello"}},
	}

	seq := &seqTestProvider{
		replies: []struct {
			resp *model.ChatResponse
			err  error
		}{
			{resp: &model.ChatResponse{Content: "rolled-up summary", StopReason: model.StopReasonEnd}},
			{resp: &model.ChatResponse{Content: "final answer", StopReason: model.StopReasonEnd}},
		},
	}

	a, err := New("a1", "T").
		WithModel(seq).
		WithStorage(store).
		WithContextConfig(ContextConfig{
			MaxContextTokens:    24,
			SummarizeThreshold:  0.8,
			PreserveRecentTurns: 1,
		}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	resp, err := a.ChatWithSession(context.Background(), sid, long)
	if err != nil {
		t.Fatalf("ChatWithSession: %v", err)
	}
	if resp == nil || resp.Content != "final answer" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestChatWithSession_PersistSummaryFails(t *testing.T) {
	base := newTestStorage()
	sid := "sum-persist-fail"
	long := strings.Repeat("x", 200)
	base.sessions[sid] = &storage.Session{ID: sid, AgentID: "a1", Status: "active"}
	base.events[sid] = []*storage.Event{
		{ID: "e1", SessionID: sid, SeqNum: 1, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "a"}},
		{ID: "e2", SessionID: sid, SeqNum: 2, Type: "chat_message", Payload: map[string]any{"role": "assistant", "content": "b"}},
	}

	wrap := &failAfterNAppendStorage{testStorage: base, okLeft: 1}
	seq := &seqTestProvider{
		replies: []struct {
			resp *model.ChatResponse
			err  error
		}{
			{resp: &model.ChatResponse{Content: "summary", StopReason: model.StopReasonEnd}},
		},
	}
	a, _ := New("a1", "T").WithModel(seq).WithStorage(wrap).WithContextConfig(ContextConfig{
		MaxContextTokens: 20, SummarizeThreshold: 0.8, PreserveRecentTurns: 1,
	}).Build()

	_, err := a.ChatWithSession(context.Background(), sid, long)
	if err == nil || !strings.Contains(err.Error(), "persist summary") {
		t.Fatalf("want persist summary error, got %v", err)
	}
}

func TestChatWithSession_SummarizeModelError(t *testing.T) {
	store := newTestStorage()
	sid := "sum-model-err"
	long := strings.Repeat("y", 200)
	store.sessions[sid] = &storage.Session{ID: sid, AgentID: "a1", Status: "active"}
	store.events[sid] = []*storage.Event{
		{ID: "e1", SessionID: sid, SeqNum: 1, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "a"}},
		{ID: "e2", SessionID: sid, SeqNum: 2, Type: "chat_message", Payload: map[string]any{"role": "assistant", "content": "b"}},
	}
	seq := &seqTestProvider{
		replies: []struct {
			resp *model.ChatResponse
			err  error
		}{{err: errors.New("summarizer model down")}},
	}
	a, _ := New("a1", "T").WithModel(seq).WithStorage(store).WithContextConfig(ContextConfig{
		MaxContextTokens: 20, SummarizeThreshold: 0.8, PreserveRecentTurns: 1,
	}).Build()

	_, err := a.ChatWithSession(context.Background(), sid, long)
	if err == nil || !strings.Contains(err.Error(), "summarize") {
		t.Fatalf("expected summarize error, got %v", err)
	}
}

func TestChatWithSession_ToolCallRoundTrip(t *testing.T) {
	store := newTestStorage()
	seq := &seqTestProvider{
		replies: []struct {
			resp *model.ChatResponse
			err  error
		}{
			{resp: &model.ChatResponse{
				StopReason: model.StopReasonToolCall,
				ToolCalls:  []model.ToolCall{{ID: "1", Name: "ping", Arguments: "{}"}},
			}},
			{resp: &model.ChatResponse{Content: "after tool", StopReason: model.StopReasonEnd}},
		},
	}
	a, _ := New("a1", "T").WithModel(seq).WithStorage(store).Build()
	a.Tools.Register(&tool.Definition{
		Name:        "ping",
		Description: "ping",
		Permission:  tool.PermAllow,
		Parameters:  map[string]any{"type": "object"},
		Handler: func(context.Context, map[string]any) (any, error) {
			return map[string]any{"ok": true}, nil
		},
	})

	resp, err := a.ChatWithSession(context.Background(), "tool-sess", "invoke")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "after tool" {
		t.Fatalf("got %q", resp.Content)
	}
}

func TestChatWithSession_OutputGuardrailBlocks(t *testing.T) {
	store := newTestStorage()
	p := &testProvider{response: &model.ChatResponse{Content: "BLOCKME token", StopReason: model.StopReasonEnd}}
	a, _ := New("a1", "T").WithModel(p).WithStorage(store).Build()
	a.Guardrails.AddRule(guardrails.Rule{
		Name: "out", Position: guardrails.Output,
		Guardrail: &guardrails.BlocklistGuardrail{Blocklist: []string{"BLOCKME"}},
	})

	_, err := a.ChatWithSession(context.Background(), "gr-sess", "hi")
	if err == nil || !strings.Contains(err.Error(), "output guardrail") {
		t.Fatalf("expected output guardrail error, got %v", err)
	}
}

func TestChatWithSession_OutputSchemaMismatch(t *testing.T) {
	store := newTestStorage()
	p := &testProvider{response: &model.ChatResponse{Content: `{"foo":1}`, StopReason: model.StopReasonEnd}}
	a, _ := New("a1", "T").WithModel(p).WithStorage(store).WithOutputSchema(map[string]any{
		"properties": map[string]any{
			"answer": map[string]any{"type": "string"},
		},
		"required": []any{"answer"},
	}).Build()

	_, err := a.ChatWithSession(context.Background(), "schema-sess", "hi")
	if err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("expected schema error, got %v", err)
	}
}

func TestChat_ModelCallRetryHookClearsError(t *testing.T) {
	p := &testProvider{err: errors.New("transient")}
	a := newTestAgent("a1", p)
	a.Hooks = append(a.Hooks, modelRetryHook{})

	resp, err := a.Chat(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "recovered-by-hook" {
		t.Fatalf("got %q", resp.Content)
	}
}

func TestChat_InstructionsFnAndExamples(t *testing.T) {
	p := &testProvider{response: &model.ChatResponse{Content: "ok", StopReason: model.StopReasonEnd}}
	a, _ := New("a1", "T").
		WithModel(p).
		WithInstructionsFn(func(_ context.Context, _ map[string]any) []string {
			return []string{"from-fn"}
		}).
		AddExample("q1", "a1").
		Build()

	_, err := a.Chat(context.Background(), "user turn")
	if err != nil {
		t.Fatal(err)
	}
	req := p.lastReq
	if req == nil || len(req.Messages) < 4 {
		t.Fatalf("expected rich message list, got %d", len(req.Messages))
	}
}

func TestChat_MaxIterationsStopsToolLoop(t *testing.T) {
	p := &testProvider{
		response: &model.ChatResponse{
			StopReason: model.StopReasonToolCall,
			ToolCalls:  []model.ToolCall{{ID: "x", Name: "loop", Arguments: "{}"}},
		},
	}
	a, _ := New("a1", "T").WithModel(p).WithMaxIterations(1).Build()
	a.Tools.Register(&tool.Definition{
		Name: "loop", Permission: tool.PermAllow,
		Parameters: map[string]any{"type": "object"},
		Handler: func(context.Context, map[string]any) (any, error) {
			return "n", nil
		},
	})

	resp, err := a.Chat(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != model.StopReasonToolCall {
		t.Fatalf("expected to stop mid-loop, got %v", resp.StopReason)
	}
}

func TestChat_BrokerAndTracerOnError(t *testing.T) {
	p := &testProvider{err: errors.New("boom")}
	br := stream.NewBroker()
	col := chronostrace.NewCollector(newTestStorage())
	a := newTestAgent("a1", p)
	a.Broker = br
	a.Tracer = col

	_, err := a.Chat(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRun_ModelOnly_StateToPrompt(t *testing.T) {
	p := &testProvider{response: &model.ChatResponse{Content: "from map", StopReason: model.StopReasonEnd}}
	a := newTestAgent("a1", p)

	st, err := a.Run(context.Background(), map[string]any{"task": "alpha", "_hidden": "skip"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != graph.RunStatusCompleted {
		t.Fatalf("status %v", st.Status)
	}
	v, _ := st.State["response"].(string)
	if v != "from map" {
		t.Fatalf("response %q", v)
	}
}

func TestCloseMCP_TwoClientsWithoutConnect(t *testing.T) {
	c1, err := mcp.NewClient(mcp.ServerConfig{Name: "c1", Command: "true"})
	if err != nil {
		t.Fatal(err)
	}
	c2, err := mcp.NewClient(mcp.ServerConfig{Name: "c2", Command: "true"})
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{MCPClients: []*mcp.Client{c1, c2}}
	a.CloseMCP()
}
