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
