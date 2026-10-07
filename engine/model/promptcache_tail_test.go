package model

import "testing"

// TestAnthropic_UncachedTailFollowsLastBreakpoint: an Uncached message is
// neither hoisted into system nor cached; it trails the final user message
// after the block carrying the last cache breakpoint.
func TestAnthropic_UncachedTailFollowsLastBreakpoint(t *testing.T) {
	p := NewAnthropic("test")
	body := p.buildRequestBody(&ChatRequest{Messages: []Message{
		{Role: RoleSystem, Content: "static prompt"},
		{Role: RoleUser, Content: "do it"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "t1", Name: "shell", Arguments: `{"command":"ls"}`}}},
		{Role: RoleTool, ToolCallID: "t1", Content: "a.go"},
		{Role: RoleSystem, Content: "Current task plan: [~] 1. step", Uncached: true},
	}}, false)

	system, _ := body["system"].([]map[string]any)
	if len(system) != 1 || system[0]["text"] != "static prompt" {
		t.Fatalf("system = %#v, want only the static prompt", body["system"])
	}
	msgs, _ := body["messages"].([]map[string]any)
	last, _ := msgs[len(msgs)-1]["content"].([]map[string]any)
	if len(msgs) != 3 || len(last) != 2 {
		t.Fatalf("messages = %#v, want the tail inside the final tool-result message", msgs)
	}
	if last[0]["type"] != "tool_result" || last[0]["cache_control"] == nil {
		t.Fatalf("last cached block = %#v, want the tool result with the breakpoint", last[0])
	}
	if last[1]["text"] != "Current task plan: [~] 1. step" || last[1]["cache_control"] != nil {
		t.Fatalf("tail block = %#v, want uncached plan text", last[1])
	}
}

func TestAnthropic_UncachedTailAfterAssistantAddsUserMessage(t *testing.T) {
	p := NewAnthropic("test")
	body := p.buildRequestBody(&ChatRequest{DisablePromptCache: true, Messages: []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, Content: "hello"},
		{Role: RoleSystem, Content: "tail", Uncached: true},
	}}, false)
	msgs, _ := body["messages"].([]map[string]any)
	if len(msgs) != 3 || msgs[2]["role"] != RoleUser {
		t.Fatalf("messages = %#v, want a trailing user message with the tail", msgs)
	}
	if _, hasSystem := body["system"]; hasSystem {
		t.Fatalf("system = %#v, want none", body["system"])
	}
}

func TestAnthropic_PrefixCacheTTLAppliesToToolsAndSystemOnly(t *testing.T) {
	p := NewAnthropicWithConfig(ProviderConfig{APIKey: "test", PromptCacheTTL: "1h"})
	body := p.buildRequestBody(&ChatRequest{
		Tools: []ToolDefinition{{Type: "function", Function: FunctionDef{Name: "shell", Parameters: map[string]any{"type": "object"}}}},
		Messages: []Message{
			{Role: RoleSystem, Content: "static prompt"},
			{Role: RoleSystem, Content: "pinned project docs"},
			{Role: RoleUser, Content: "do it"},
		},
	}, false)

	tools, _ := body["tools"].([]map[string]any)
	if cc, _ := tools[0]["cache_control"].(map[string]any); cc["ttl"] != "1h" {
		t.Fatalf("tool cache_control = %#v, want ttl 1h", tools[0]["cache_control"])
	}
	system, _ := body["system"].([]map[string]any)
	if len(system) != 2 {
		t.Fatalf("system = %#v, want two blocks", body["system"])
	}
	for i, block := range system {
		if cc, _ := block["cache_control"].(map[string]any); cc["ttl"] != "1h" {
			t.Fatalf("system[%d] cache_control = %#v, want ttl 1h", i, block["cache_control"])
		}
	}
	msgs, _ := body["messages"].([]map[string]any)
	last, _ := msgs[len(msgs)-1]["content"].([]map[string]any)
	if cc, _ := last[0]["cache_control"].(map[string]any); cc == nil || cc["ttl"] != nil {
		t.Fatalf("message cache_control = %#v, want the default 5-minute checkpoint", last[0]["cache_control"])
	}
}

func TestAnthropic_DefaultPrefixCacheKeepsThreeBreakpoints(t *testing.T) {
	p := NewAnthropic("test")
	body := p.buildRequestBody(&ChatRequest{Messages: []Message{
		{Role: RoleSystem, Content: "static prompt"},
		{Role: RoleSystem, Content: "pinned project docs"},
		{Role: RoleUser, Content: "do it"},
	}}, false)
	system, _ := body["system"].([]map[string]any)
	if cc, _ := system[0]["cache_control"].(map[string]any); cc == nil || cc["ttl"] != nil {
		t.Fatalf("system[0] cache_control = %#v, want default checkpoint", system[0]["cache_control"])
	}
	if system[1]["cache_control"] != nil {
		t.Fatalf("system[1] cache_control = %#v, want none without a TTL", system[1]["cache_control"])
	}
}

func TestAnthropic_TailCacheTTL(t *testing.T) {
	tailTTL := func(cfg ProviderConfig) any {
		t.Helper()
		cfg.APIKey = "test"
		body := NewAnthropicWithConfig(cfg).buildRequestBody(&ChatRequest{Messages: []Message{
			{Role: RoleSystem, Content: "static prompt"},
			{Role: RoleUser, Content: "do it"},
		}}, false)
		msgs, _ := body["messages"].([]map[string]any)
		last, _ := msgs[len(msgs)-1]["content"].([]map[string]any)
		cc, _ := last[len(last)-1]["cache_control"].(map[string]any)
		if cc == nil {
			t.Fatalf("last message has no cache breakpoint: %#v", msgs)
		}
		return cc["ttl"]
	}
	if got := tailTTL(ProviderConfig{PromptCacheTTL: "1h", PromptCacheTailTTL: "1h"}); got != "1h" {
		t.Errorf("tail ttl = %v, want 1h", got)
	}
	if got := tailTTL(ProviderConfig{PromptCacheTTL: "1h"}); got != nil {
		t.Errorf("tail ttl without the tail setting = %v, want default", got)
	}
	// A one-hour tail after a five-minute prefix is rejected by the API.
	if got := tailTTL(ProviderConfig{PromptCacheTailTTL: "1h"}); got != nil {
		t.Errorf("tail ttl with a default prefix = %v, want default", got)
	}
}
