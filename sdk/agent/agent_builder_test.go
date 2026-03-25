package agent

import (
	"context"
	"testing"

	"github.com/spawn08/chronos/engine/guardrails"
	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/mcp"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/storage"
)

// ---------------------------------------------------------------------------
// Mock provider for tests
// ---------------------------------------------------------------------------

type builderTestProvider struct {
	name  string
	model string
	resp  string
}

func (p *builderTestProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	return &model.ChatResponse{Content: p.resp}, nil
}

func (p *builderTestProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	ch <- &model.ChatResponse{Content: p.resp}
	close(ch)
	return ch, nil
}

func (p *builderTestProvider) Name() string  { return p.name }
func (p *builderTestProvider) Model() string { return p.model }

// ---------------------------------------------------------------------------
// Builder tests
// ---------------------------------------------------------------------------

func TestBuilder_WithUserID(t *testing.T) {
	a, err := New("agent1", "Agent").
		WithUserID("user-123").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.UserID != "user-123" {
		t.Errorf("UserID=%q", a.UserID)
	}
}

func TestBuilder_WithMaxIterations(t *testing.T) {
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		WithMaxIterations(10).
		Build()
	if a.MaxIterations != 10 {
		t.Errorf("MaxIterations=%d", a.MaxIterations)
	}
}

func TestBuilder_WithReasoningModel(t *testing.T) {
	reasoner := &builderTestProvider{name: "reasoner", model: "r", resp: "ok"}
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		WithReasoningModel(reasoner).
		Build()
	if a.ReasoningModel == nil {
		t.Error("ReasoningModel should be set")
	}
	if a.ReasoningModel.Name() != "reasoner" {
		t.Errorf("ReasoningModel.Name=%q", a.ReasoningModel.Name())
	}
}

func TestBuilder_WithContextConfig(t *testing.T) {
	cfg := ContextConfig{
		MaxContextTokens:    8000,
		SummarizeThreshold:  0.7,
		PreserveRecentTurns: 3,
	}
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		WithContextConfig(cfg).
		Build()
	if a.ContextCfg.MaxContextTokens != 8000 {
		t.Errorf("MaxContextTokens=%d", a.ContextCfg.MaxContextTokens)
	}
}

func TestBuilder_WithDebug(t *testing.T) {
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		WithDebug(true).
		Build()
	if !a.Debug {
		t.Error("Debug should be true")
	}
}

func TestBuilder_WithInstructionsFn(t *testing.T) {
	called := false
	fn := func(ctx context.Context, state map[string]any) []string {
		called = true
		return []string{"be helpful"}
	}
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		WithInstructionsFn(fn).
		Build()
	if a.InstructionsFn == nil {
		t.Error("InstructionsFn should be set")
	}
	// Call it to verify
	result := a.InstructionsFn(context.Background(), nil)
	if !called {
		t.Error("InstructionsFn should have been called")
	}
	if len(result) != 1 || result[0] != "be helpful" {
		t.Errorf("result=%v", result)
	}
}

func TestBuilder_AddExample(t *testing.T) {
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		AddExample("input 1", "output 1").
		AddExample("input 2", "output 2").
		Build()
	if len(a.Examples) != 2 {
		t.Errorf("Examples count=%d", len(a.Examples))
	}
	if a.Examples[0].Input != "input 1" {
		t.Errorf("Example[0].Input=%q", a.Examples[0].Input)
	}
}

func TestBuilder_AddTool(t *testing.T) {
	def := &tool.Definition{
		Name:        "my_tool",
		Description: "A test tool",
		Handler: func(_ context.Context, _ map[string]any) (any, error) {
			return "result", nil
		},
	}
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		AddTool(def).
		Build()
	if a.Tools == nil {
		t.Error("Tools should not be nil")
	}
}

func TestBuilder_AddToolkit(t *testing.T) {
	tk := tool.NewToolkit("test-toolkit", "Test toolkit")
	tk.Add(&tool.Definition{
		Name:    "tk_tool",
		Handler: func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	})
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		AddToolkit(tk).
		Build()
	if a.Tools == nil {
		t.Error("Tools should not be nil after AddToolkit")
	}
}

func TestBuilder_AddHook(t *testing.T) {
	hook := &hooks.LoggingHook{}
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		AddHook(hook).
		Build()
	if len(a.Hooks) != 1 {
		t.Errorf("Hooks count=%d, want 1", len(a.Hooks))
	}
}

func TestBuilder_AddInputGuardrail(t *testing.T) {
	g := &guardrails.BlocklistGuardrail{Blocklist: []string{"bad-word"}}
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		AddInputGuardrail("blocklist", g).
		Build()
	if a.Guardrails == nil {
		t.Error("Guardrails should not be nil")
	}
}

func TestBuilder_AddOutputGuardrail(t *testing.T) {
	g := &guardrails.BlocklistGuardrail{Blocklist: []string{"bad-word"}}
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		AddOutputGuardrail("output-blocklist", g).
		Build()
	if a.Guardrails == nil {
		t.Error("Guardrails should not be nil")
	}
}

func TestBuilder_WithBroker_Extra(t *testing.T) {
	broker := stream.NewBroker()
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		WithBroker(broker).
		Build()
	if a.Broker == nil {
		t.Error("Broker should not be nil")
	}
}

func TestBuilder_WithStorage(t *testing.T) {
	// We use nil here since we just test the builder sets the field
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		WithStorage(nil).
		Build()
	if a.Storage != nil {
		t.Errorf("Storage should be nil when set to nil")
	}
}

func TestBuilder_AddMCPServer_Valid(t *testing.T) {
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		AddMCPServer(mcp.ServerConfig{Name: "echo-server", Transport: mcp.TransportStdio, Command: "echo", Args: []string{"hello"}}).
		Build()
	if len(a.MCPClients) != 1 {
		t.Errorf("MCPClients count=%d, want 1", len(a.MCPClients))
	}
}

func TestBuilder_AddMCPServer_Invalid(t *testing.T) {
	// Invalid config (SSE transport not supported) should not add a client
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		AddMCPServer(mcp.ServerConfig{Name: "test", Transport: mcp.TransportSSE, URL: "http://localhost"}).
		Build()
	if len(a.MCPClients) != 0 {
		t.Errorf("MCPClients count=%d, want 0 for invalid config", len(a.MCPClients))
	}
}

func TestBuilder_AddSubAgent_Extra(t *testing.T) {
	sub, _ := New("sub", "Sub").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		Build()
	a, _ := New("parent", "Parent").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		AddSubAgent(sub).
		Build()
	if len(a.SubAgents) != 1 {
		t.Errorf("SubAgents count=%d", len(a.SubAgents))
	}
}

func TestBuilder_ConnectMCP_NotConnected(t *testing.T) {
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		Build()
	// No MCP clients - should do nothing
	err := a.ConnectMCP(context.Background())
	if err != nil {
		t.Logf("ConnectMCP error (expected if clients not found): %v", err)
	}
}

func TestBuilder_CloseMCP(t *testing.T) {
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		Build()
	// No MCP clients - should do nothing
	a.CloseMCP()
}

// ---------------------------------------------------------------------------
// Reasoning tests
// ---------------------------------------------------------------------------

func TestWithReasoning_CoT(t *testing.T) {
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		WithReasoning(ReasoningCoT).
		Build()
	if a.Reasoning != ReasoningCoT {
		t.Errorf("Reasoning=%d", a.Reasoning)
	}
}

func TestApplyReasoning_None(t *testing.T) {
	msgs := []model.Message{{Role: model.RoleUser, Content: "hi"}}
	result := applyReasoning(ReasoningNone, msgs)
	if len(result) != 1 {
		t.Errorf("expected no extra messages, got %d", len(result))
	}
}

func TestApplyReasoning_CoT(t *testing.T) {
	msgs := []model.Message{{Role: model.RoleUser, Content: "hi"}}
	result := applyReasoning(ReasoningCoT, msgs)
	if len(result) != 2 {
		t.Errorf("expected 2 messages (user + CoT prompt), got %d", len(result))
	}
}

func TestApplyReasoning_Reflection(t *testing.T) {
	msgs := []model.Message{{Role: model.RoleUser, Content: "hi"}}
	result := applyReasoning(ReasoningReflection, msgs)
	if len(result) != 2 {
		t.Errorf("expected 2 messages (user + reflection prompt), got %d", len(result))
	}
}

func TestExtractReasoningParts_Full(t *testing.T) {
	content := `<think>I think about it</think><critique>Looks good</critique><answer>42</answer>`
	parts := ExtractReasoningParts(content)
	if parts["think"] != "I think about it" {
		t.Errorf("think=%q", parts["think"])
	}
	if parts["critique"] != "Looks good" {
		t.Errorf("critique=%q", parts["critique"])
	}
	if parts["answer"] != "42" {
		t.Errorf("answer=%q", parts["answer"])
	}
}

func TestExtractReasoningParts_Partial(t *testing.T) {
	content := `<answer>just an answer</answer>`
	parts := ExtractReasoningParts(content)
	if parts["answer"] != "just an answer" {
		t.Errorf("answer=%q", parts["answer"])
	}
	if parts["think"] != "" {
		t.Errorf("think should be empty, got %q", parts["think"])
	}
}

func TestExtractReasoningParts_Empty(t *testing.T) {
	parts := ExtractReasoningParts("no tags here")
	if parts["think"] != "" || parts["critique"] != "" || parts["answer"] != "" {
		t.Errorf("all parts should be empty: %v", parts)
	}
}

// ---------------------------------------------------------------------------
// Session helpers
// ---------------------------------------------------------------------------

func TestStrFromMap(t *testing.T) {
	m := map[string]any{"key": "value", "num": 42}
	if v := strFromMap(m, "key"); v != "value" {
		t.Errorf("strFromMap(key)=%q", v)
	}
	if v := strFromMap(m, "num"); v != "" {
		t.Errorf("strFromMap(num)=%q (non-string should return empty)", v)
	}
	if v := strFromMap(m, "missing"); v != "" {
		t.Errorf("strFromMap(missing)=%q", v)
	}
}

func TestChatSessionFromEvents_Empty(t *testing.T) {
	cs := chatSessionFromEvents(nil)
	if cs == nil {
		t.Fatal("expected non-nil ChatSession")
	}
	if len(cs.Messages) != 0 {
		t.Errorf("expected no messages, got %d", len(cs.Messages))
	}
}

func TestChatSessionFromEvents_Messages(t *testing.T) {
	events := []*storage.Event{
		{
			Type: "chat_message",
			Payload: map[string]any{
				"role":    "user",
				"content": "hello",
			},
		},
		{
			Type: "chat_message",
			Payload: map[string]any{
				"role":    "assistant",
				"content": "hi there",
			},
		},
		{
			Type: "chat_summary",
			Payload: map[string]any{
				"summary": "user said hello",
			},
		},
	}

	cs := chatSessionFromEvents(events)
	if len(cs.Messages) != 2 {
		t.Errorf("messages count=%d, want 2", len(cs.Messages))
	}
	if cs.Summary != "user said hello" {
		t.Errorf("summary=%q", cs.Summary)
	}
}

func TestChatSessionFromEvents_WithToolCalls(t *testing.T) {
	events := []*storage.Event{
		{
			Type: "chat_message",
			Payload: map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"id":        "tc1",
						"name":      "search",
						"arguments": `{"q":"test"}`,
					},
				},
			},
		},
		{
			Type: "chat_message",
			Payload: map[string]any{
				"role":         "tool",
				"content":      "search result",
				"name":         "search",
				"tool_call_id": "tc1",
			},
		},
	}

	cs := chatSessionFromEvents(events)
	if len(cs.Messages) != 2 {
		t.Errorf("messages count=%d", len(cs.Messages))
	}
	if len(cs.Messages[0].ToolCalls) != 1 {
		t.Errorf("tool_calls count=%d", len(cs.Messages[0].ToolCalls))
	}
	if cs.Messages[0].ToolCalls[0].Name != "search" {
		t.Errorf("tool name=%q", cs.Messages[0].ToolCalls[0].Name)
	}
	if cs.Messages[1].ToolCallID != "tc1" {
		t.Errorf("tool_call_id=%q", cs.Messages[1].ToolCallID)
	}
}

func TestChatSessionFromEvents_InvalidPayload(t *testing.T) {
	events := []*storage.Event{
		{Type: "chat_message", Payload: "not a map"},      // should be skipped
		{Type: "unknown_type", Payload: map[string]any{}}, // should be skipped
	}
	cs := chatSessionFromEvents(events)
	if len(cs.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(cs.Messages))
	}
}

// ---------------------------------------------------------------------------
// debugLog test
// ---------------------------------------------------------------------------

func TestDebugLog(t *testing.T) {
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		WithDebug(true).
		Build()
	// debugLog when Debug=true should not panic
	a.debugLog("test message: %s", "hello")
}

func TestDebugLog_Disabled(t *testing.T) {
	a, _ := New("a", "A").
		WithModel(&builderTestProvider{name: "test", model: "m", resp: "ok"}).
		Build()
	// debugLog when Debug=false should do nothing
	a.debugLog("should not print: %s", "anything")
}
