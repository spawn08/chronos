package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

func TestPersistMessage_Basic(t *testing.T) {
	store := newTestStorage()
	msg := model.Message{Role: model.RoleUser, Content: "hello"}
	err := persistMessage(context.Background(), store, "sess-1", 1, msg)
	if err != nil {
		t.Fatalf("persistMessage: %v", err)
	}
	evts := store.events["sess-1"]
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0].Type != "chat_message" {
		t.Errorf("unexpected type: %q", evts[0].Type)
	}
}

func TestPersistMessage_WithToolCalls(t *testing.T) {
	store := newTestStorage()
	msg := model.Message{
		Role:    model.RoleAssistant,
		Content: "",
		ToolCalls: []model.ToolCall{
			{ID: "tc-1", Name: "my_tool", Arguments: `{"x":1}`},
		},
	}
	err := persistMessage(context.Background(), store, "sess-2", 1, msg)
	if err != nil {
		t.Fatalf("persistMessage with tool calls: %v", err)
	}
	evts := store.events["sess-2"]
	payload, ok := evts[0].Payload.(map[string]any)
	if !ok {
		t.Fatal("expected map payload")
	}
	if _, ok := payload["tool_calls"]; !ok {
		t.Error("expected tool_calls in payload")
	}
}

func TestPersistMessage_WithNameAndToolCallID(t *testing.T) {
	store := newTestStorage()
	msg := model.Message{
		Role:       model.RoleTool,
		Content:    "result",
		Name:       "my_tool",
		ToolCallID: "tc-1",
	}
	err := persistMessage(context.Background(), store, "sess-3", 1, msg)
	if err != nil {
		t.Fatalf("persistMessage: %v", err)
	}
	evts := store.events["sess-3"]
	payload, _ := evts[0].Payload.(map[string]any)
	if payload["name"] != "my_tool" {
		t.Errorf("expected name=my_tool, got %v", payload["name"])
	}
	if payload["tool_call_id"] != "tc-1" {
		t.Errorf("expected tool_call_id=tc-1, got %v", payload["tool_call_id"])
	}
}

func TestPersistSummary(t *testing.T) {
	store := newTestStorage()
	err := persistSummary(context.Background(), store, "sess-sum", 1, "this is a summary")
	if err != nil {
		t.Fatalf("persistSummary: %v", err)
	}
	evts := store.events["sess-sum"]
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0].Type != "chat_summary" {
		t.Errorf("unexpected type: %q", evts[0].Type)
	}
}

func TestCompactSession_NoModel(t *testing.T) {
	a := &Agent{ID: "a1"}
	err := a.CompactSession(context.Background(), "sess")
	if err == nil || !strings.Contains(err.Error(), "no model") {
		t.Fatalf("expected 'no model' error, got %v", err)
	}
}

func TestCompactSession_NoStorage(t *testing.T) {
	a, err := New("a1", "T").WithModel(&fakeChatProvider{content: "summary"}).Build()
	if err != nil {
		t.Fatal(err)
	}
	err = a.CompactSession(context.Background(), "sess")
	if err == nil || !strings.Contains(err.Error(), "no storage") {
		t.Fatalf("expected 'no storage' error, got %v", err)
	}
}

func TestCompactSession_EmptySessionIsNoop(t *testing.T) {
	store := newTestStorage()
	a, err := New("a1", "T").WithModel(&fakeChatProvider{content: "summary"}).WithStorage(store).Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CompactSession(context.Background(), "no-such-session"); err != nil {
		t.Fatalf("expected no-op nil error for an empty session, got %v", err)
	}
}

func TestCompactSession_SummarizesRegardlessOfContextSize(t *testing.T) {
	store := newTestStorage()
	sid := "compact-sess"
	store.sessions[sid] = &storage.Session{ID: sid, AgentID: "a1", Status: "active"}
	store.events[sid] = []*storage.Event{
		{ID: "e1", SessionID: sid, SeqNum: 1, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "investigate the 7 bottlenecks"}},
		{ID: "e2", SessionID: sid, SeqNum: 2, Type: "chat_message", Payload: map[string]any{"role": "assistant", "content": "found bottleneck 1"}},
		{ID: "e3", SessionID: sid, SeqNum: 3, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "keep going"}},
		{ID: "e4", SessionID: sid, SeqNum: 4, Type: "chat_message", Payload: map[string]any{"role": "assistant", "content": "found bottleneck 2"}},
	}

	// PreserveRecentTurns: 1 keeps only the last 2 messages verbatim, so with
	// 4 messages here there's still something older to actually summarize.
	// This session's real conversation is tiny either way, nowhere near any
	// context-window threshold, so the automatic inline compaction in
	// ChatWithSession would never fire on its own. CompactSession must
	// summarize anyway, since it's being asked to recover from an unrelated
	// failure (a budget cap), not a context-window limit.
	a, err := New("a1", "T").
		WithModel(&fakeChatProvider{content: "rolled-up summary"}).
		WithStorage(store).
		WithContextConfig(ContextConfig{PreserveRecentTurns: 1}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	if err := a.CompactSession(context.Background(), sid); err != nil {
		t.Fatalf("CompactSession: %v", err)
	}

	events := store.events[sid]
	last := events[len(events)-1]
	if last.Type != "chat_summary" {
		t.Fatalf("expected a chat_summary event to be appended, last event type = %q", last.Type)
	}
	payload := last.Payload.(map[string]any)
	if payload["summary"] != "rolled-up summary" {
		t.Errorf("summary = %v, want %q", payload["summary"], "rolled-up summary")
	}
}

func TestCompactSession_SummarizeErrorPropagates(t *testing.T) {
	store := newTestStorage()
	sid := "compact-err-sess"
	store.sessions[sid] = &storage.Session{ID: sid, AgentID: "a1", Status: "active"}
	store.events[sid] = []*storage.Event{
		{ID: "e1", SessionID: sid, SeqNum: 1, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "hi"}},
		{ID: "e2", SessionID: sid, SeqNum: 2, Type: "chat_message", Payload: map[string]any{"role": "assistant", "content": "hello"}},
		{ID: "e3", SessionID: sid, SeqNum: 3, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "continue"}},
	}

	a, err := New("a1", "T").
		WithModel(&fakeChatProvider{err: errors.New("model unavailable")}).
		WithStorage(store).
		WithContextConfig(ContextConfig{PreserveRecentTurns: 1}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	err = a.CompactSession(context.Background(), sid)
	if err == nil || !strings.Contains(err.Error(), "summarize") {
		t.Fatalf("expected summarize error, got %v", err)
	}
}

// fakeChatProvider is a minimal model.Provider stub for CompactSession tests
// that don't need the multi-reply sequencing seqTestProvider provides.
type fakeChatProvider struct {
	content string
	err     error
}

func (f *fakeChatProvider) Chat(context.Context, *model.ChatRequest) (*model.ChatResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &model.ChatResponse{Content: f.content, StopReason: model.StopReasonEnd}, nil
}
func (f *fakeChatProvider) StreamChat(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeChatProvider) Name() string  { return "fake" }
func (f *fakeChatProvider) Model() string { return "fake-model" }

func TestChatWithSession_NoModel(t *testing.T) {
	a := &Agent{ID: "a1"}
	_, err := a.ChatWithSession(context.Background(), "sess", "hello")
	if err == nil {
		t.Fatal("expected error for no model")
		return
	}
}

func TestChatWithSession_NoStorage(t *testing.T) {
	a := &Agent{
		ID:    "a1",
		Model: &testProvider{response: &model.ChatResponse{Content: "hi"}},
	}
	_, err := a.ChatWithSession(context.Background(), "sess", "hello")
	if err == nil {
		t.Fatal("expected error for no storage")
		return
	}
}

func TestChatWithSession_Success(t *testing.T) {
	store := newTestStorage()
	prov := &testProvider{response: &model.ChatResponse{Content: "hello back", StopReason: model.StopReasonEnd}}
	a, _ := New("a1", "Test").WithModel(prov).WithStorage(store).Build()

	resp, err := a.ChatWithSession(context.Background(), "test-session", "hello")
	if err != nil {
		t.Fatalf("ChatWithSession: %v", err)
	}
	if resp.Content != "hello back" {
		t.Errorf("unexpected content: %q", resp.Content)
	}
}

func TestChatWithSession_ExistingSession(t *testing.T) {
	store := newTestStorage()
	// Pre-create session so GetSession succeeds
	store.sessions["existing-sess"] = &storage.Session{ID: "existing-sess", AgentID: "a1", Status: "active"}
	// Add a prior event
	store.events["existing-sess"] = []*storage.Event{
		{
			ID: "e1", SessionID: "existing-sess", SeqNum: 1, Type: "chat_message",
			Payload: map[string]any{"role": "user", "content": "prior message"},
		},
	}
	prov := &testProvider{response: &model.ChatResponse{Content: "reply", StopReason: model.StopReasonEnd}}
	a, _ := New("a1", "Test").WithModel(prov).WithStorage(store).Build()

	resp, err := a.ChatWithSession(context.Background(), "existing-sess", "follow-up")
	if err != nil {
		t.Fatalf("ChatWithSession with existing session: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
		return
	}
}

func TestChatWithSession_ModelError(t *testing.T) {
	store := newTestStorage()
	prov := &testProvider{err: errors.New("model failed")}
	a, _ := New("a1", "Test").WithModel(prov).WithStorage(store).Build()

	_, err := a.ChatWithSession(context.Background(), "sess", "hello")
	if err == nil {
		t.Fatal("expected error from model failure")
		return
	}
}

func TestChatWithSession_WithSummary(t *testing.T) {
	store := newTestStorage()
	// Pre-create session with a summary event
	store.sessions["sum-sess"] = &storage.Session{ID: "sum-sess", AgentID: "a1", Status: "active"}
	store.events["sum-sess"] = []*storage.Event{
		{
			ID: "e1", SessionID: "sum-sess", SeqNum: 1, Type: "chat_summary",
			Payload: "This is a prior summary.",
		},
	}
	prov := &testProvider{response: &model.ChatResponse{Content: "continuing after summary", StopReason: model.StopReasonEnd}}
	a, _ := New("a1", "Test").WithModel(prov).WithStorage(store).Build()

	resp, err := a.ChatWithSession(context.Background(), "sum-sess", "continue")
	if err != nil {
		t.Fatalf("ChatWithSession with summary: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
		return
	}
}

func TestBuildSystemContext_WithSystemPromptAndInstructions(t *testing.T) {
	a := &Agent{
		SystemPrompt: "You are helpful",
		Instructions: []string{"Be concise", "Use simple language"},
	}
	msgs := a.buildSystemContext(context.Background(), "test query")
	// 1 system prompt + 2 instructions
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "You are helpful" {
		t.Errorf("unexpected system prompt: %q", msgs[0].Content)
	}
}

func TestBuildSystemContext_Empty(t *testing.T) {
	a := &Agent{}
	msgs := a.buildSystemContext(context.Background(), "test")
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestResolveContextLimit_WithConfig(t *testing.T) {
	a := &Agent{
		Model:      &testProvider{},
		ContextCfg: ContextConfig{MaxContextTokens: 8000},
	}
	limit := a.resolveContextLimit()
	if limit != 8000 {
		t.Errorf("expected 8000, got %d", limit)
	}
}

func TestResolveContextLimit_Default(t *testing.T) {
	a := &Agent{
		Model:      &testProvider{},
		ContextCfg: ContextConfig{},
	}
	limit := a.resolveContextLimit()
	// Should return some non-zero default from model.ContextLimit
	if limit <= 0 {
		t.Errorf("expected positive limit, got %d", limit)
	}
}

func TestBuildSystemContext_WithKnowledge(t *testing.T) {
	a := &Agent{
		SystemPrompt: "helpful assistant",
		Knowledge:    &mockKnowledge{},
	}
	msgs := a.buildSystemContext(context.Background(), "search query")
	// Should have system prompt + knowledge context
	if len(msgs) < 2 {
		t.Errorf("expected at least 2 messages (prompt + knowledge), got %d", len(msgs))
	}
	hasKnowledge := false
	for _, m := range msgs {
		if len(m.Content) > 10 && m.Role == model.RoleSystem {
			if m.Content != "helpful assistant" {
				hasKnowledge = true
			}
		}
	}
	if !hasKnowledge {
		t.Error("expected knowledge context in messages")
	}
}
