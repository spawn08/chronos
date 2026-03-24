package agent

import (
	"context"
	"errors"
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

func TestChatWithSession_NoModel(t *testing.T) {
	a := &Agent{ID: "a1"}
	_, err := a.ChatWithSession(context.Background(), "sess", "hello")
	if err == nil {
		t.Fatal("expected error for no model")
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
	}
}

func TestChatWithSession_ModelError(t *testing.T) {
	store := newTestStorage()
	prov := &testProvider{err: errors.New("model failed")}
	a, _ := New("a1", "Test").WithModel(prov).WithStorage(store).Build()

	_, err := a.ChatWithSession(context.Background(), "sess", "hello")
	if err == nil {
		t.Fatal("expected error from model failure")
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
