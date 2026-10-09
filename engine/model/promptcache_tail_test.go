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
	if tools[0]["cache_control"] != nil {
		t.Fatalf("tool cache_control = %#v, want none: the system checkpoints cover tools", tools[0]["cache_control"])
	}
	system, _ := body["system"].([]map[string]any)
	if len(system) != 2 {
		t.Fatalf("system = %#v, want two blocks", body["system"])
	}
	if cc, _ := system[0]["cache_control"].(map[string]any); cc["ttl"] != "1h" {
		t.Fatalf("system[0] cache_control = %#v, want ttl 1h", system[0]["cache_control"])
	}
	// The pinned remainder carries changing context (summary, memory), so it
	// keeps the conversation's 5-minute TTL unless the tail is one-hour too.
	if cc, _ := system[1]["cache_control"].(map[string]any); cc == nil || cc["ttl"] != nil {
		t.Fatalf("system[1] cache_control = %#v, want the default 5-minute checkpoint", system[1]["cache_control"])
	}
	msgs, _ := body["messages"].([]map[string]any)
	last, _ := msgs[len(msgs)-1]["content"].([]map[string]any)
	if cc, _ := last[0]["cache_control"].(map[string]any); cc == nil || cc["ttl"] != nil {
		t.Fatalf("message cache_control = %#v, want the default 5-minute checkpoint", last[0]["cache_control"])
	}
}

// Without a TTL the pinned system context still gets its own 5-minute
// checkpoint, so a changed conversation falls back to it, not to system[0].
func TestAnthropic_DefaultPrefixCacheCheckpointsPinnedSystem(t *testing.T) {
	p := NewAnthropic("test")
	body := p.buildRequestBody(&ChatRequest{Messages: []Message{
		{Role: RoleSystem, Content: "static prompt"},
		{Role: RoleSystem, Content: "pinned project docs"},
		{Role: RoleUser, Content: "do it"},
	}}, false)
	system, _ := body["system"].([]map[string]any)
	for i, block := range system {
		if cc, _ := block["cache_control"].(map[string]any); cc == nil || cc["ttl"] != nil {
			t.Fatalf("system[%d] cache_control = %#v, want the default checkpoint", i, block["cache_control"])
		}
	}
}

// A tool round checkpoints the previous request's tail as well as the new
// one, so the prior cache entry is read however many blocks the round added,
// and the request never exceeds Anthropic's four checkpoints.
func TestAnthropic_CachesPreviousTailInToolLoop(t *testing.T) {
	p := NewAnthropic("test")
	messages := []Message{
		{Role: RoleSystem, Content: "static prompt"},
		{Role: RoleSystem, Content: "pinned project docs"},
		{Role: RoleUser, Content: "do it"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "a", Name: "read", Arguments: "{}"}}},
		{Role: RoleTool, ToolCallID: "a", Content: "first"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "b", Name: "read", Arguments: "{}"}, {ID: "c", Name: "read", Arguments: "{}"}}},
		{Role: RoleTool, ToolCallID: "b", Content: "second"},
		{Role: RoleTool, ToolCallID: "c", Content: "third"},
	}
	body := p.buildRequestBody(&ChatRequest{
		Messages: messages,
		Tools:    []ToolDefinition{{Type: "function", Function: FunctionDef{Name: "read", Parameters: map[string]any{"type": "object"}}}},
	}, false)
	msgs, _ := body["messages"].([]map[string]any)
	checkpointed := func(msg map[string]any) bool {
		content, _ := msg["content"].([]map[string]any)
		return len(content) > 0 && content[len(content)-1]["cache_control"] != nil
	}
	// messages: user, assistant, tool(a), assistant, tool(b), tool(c)
	if !checkpointed(msgs[2]) {
		t.Fatalf("previous tail (tool result a) has no checkpoint: %#v", msgs[2])
	}
	if !checkpointed(msgs[5]) {
		t.Fatalf("current tail has no checkpoint: %#v", msgs[5])
	}
	count := 0
	countBlocks := func(blocks []map[string]any) {
		for _, block := range blocks {
			if block["cache_control"] != nil {
				count++
			}
		}
	}
	countBlocks(body["tools"].([]map[string]any))
	countBlocks(body["system"].([]map[string]any))
	for _, msg := range msgs {
		if content, ok := msg["content"].([]map[string]any); ok {
			countBlocks(content)
		}
	}
	if count > 4 {
		t.Fatalf("request has %d cache checkpoints, Anthropic allows 4", count)
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

// An empty assistant reply kept in history must not become an empty text
// block carrying a checkpoint, which Anthropic rejects.
func TestAnthropic_PreviousTailSkipsEmptyContent(t *testing.T) {
	body := NewAnthropic("test").buildRequestBody(&ChatRequest{Messages: []Message{
		{Role: RoleUser, Content: "do it"},
		{Role: RoleAssistant, Content: ""},
		{Role: RoleAssistant, Content: "done"},
		{Role: RoleUser, Content: "next"},
	}}, false)
	msgs, _ := body["messages"].([]map[string]any)
	if content, ok := msgs[1]["content"].(string); !ok || content != "" {
		t.Fatalf("empty assistant content = %#v, want left as an empty string", msgs[1]["content"])
	}
}
