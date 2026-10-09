package model

import (
	"context"
	"testing"
)

func TestAnthropicThinkingBoundToConversation(t *testing.T) {
	for id, want := range map[string]bool{
		"claude-opus-5-5":              true,
		"claude-sonnet-5-5":            true,
		"claude-haiku-5-5":             true,
		"claude-fable-5-1":             true,
		"us.anthropic.claude-opus-5-5": true,
		"claude-mythos-5-1":            false,
		"claude-fable-5":               false,
		"claude-opus-5":                false,
		"claude-sonnet-5":              false,
		"claude-opus-4-8":              false,
		"claude-sonnet-4-6":            false,
		"gpt-5.5":                      false,
	} {
		if got := AnthropicThinkingBoundToConversation(id); got != want {
			t.Errorf("AnthropicThinkingBoundToConversation(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestStripAnthropicThinkingKeepsTextToolsAndOtherState(t *testing.T) {
	thinking := []map[string]any{{"type": "thinking", "thinking": "", "signature": "sig"}}
	other := map[string]any{"gemini": "signed"}
	messages := []Message{
		{Role: RoleAssistant, Content: "plan", ToolCalls: []ToolCall{{ID: "a", Name: "read"}}, ProviderState: thinking},
		{Role: RoleTool, ToolCallID: "a", Content: "out"},
		{Role: RoleAssistant, Content: "done", ProviderState: other},
	}
	got := StripAnthropicThinking(messages)
	if got[0].ProviderState != nil || got[0].Content != "plan" || len(got[0].ToolCalls) != 1 {
		t.Fatalf("stripped message = %+v, want thinking removed and text/tool calls kept", got[0])
	}
	if got[2].ProviderState == nil {
		t.Fatal("non-Anthropic provider state was removed")
	}
	if messages[0].ProviderState == nil {
		t.Fatal("StripAnthropicThinking mutated its input")
	}
}

type modelNamedSummarizer struct {
	mockSummarizerProvider
	model string
}

func (p *modelNamedSummarizer) Model() string { return p.model }

// Keep-tail compaction: the preserved turns were produced with the summarized
// turns before them, so bound models must not replay their thinking.
func TestSummarizeStripsBoundThinkingFromPreservedTail(t *testing.T) {
	thinking := []map[string]any{{"type": "thinking", "thinking": "", "signature": "sig"}}
	var messages []Message
	for i := 0; i < 8; i++ {
		messages = append(messages, Message{Role: RoleUser, Content: "q"}, Message{Role: RoleAssistant, Content: "a", ProviderState: thinking})
	}
	for model, wantStripped := range map[string]bool{"claude-opus-5-5": true, "claude-opus-4-8": false} {
		p := &modelNamedSummarizer{mockSummarizerProvider: mockSummarizerProvider{response: "summary"}, model: model}
		s := NewSummarizer(p, NewTokenCounter(model), SummarizationConfig{PreserveRecentTurns: 2})
		result, err := s.Summarize(context.Background(), "", messages)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range result.PreservedMessages {
			if m.Role == RoleAssistant && (m.ProviderState == nil) != wantStripped {
				t.Fatalf("%s: preserved assistant state = %v, want stripped=%v", model, m.ProviderState, wantStripped)
			}
		}
	}
}

// A safety-classifier refusal must not look like a normal end of turn.
func TestAnthropicRefusalMapsToContentFilter(t *testing.T) {
	if got := mapAnthropicStopReason("refusal"); got != StopReasonFilter {
		t.Fatalf("streaming refusal = %q, want %q", got, StopReasonFilter)
	}
	cr := NewAnthropic("test").convertResponse(&anthropicResponse{StopReason: "refusal"})
	if cr.StopReason != StopReasonFilter {
		t.Fatalf("refusal = %q, want %q", cr.StopReason, StopReasonFilter)
	}
}
