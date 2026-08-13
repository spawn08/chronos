package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAIReasoningEffortRequest(t *testing.T) {
	body := buildOpenAIRequestBody(&ChatRequest{
		Messages:  []Message{{Role: RoleUser, Content: "solve"}},
		Reasoning: &ReasoningConfig{Enabled: true, Effort: "high"},
	}, "gpt-test", false)
	if got := body["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %v, want high", got)
	}
}

func TestDisabledReasoningDoesNotChangeRequests(t *testing.T) {
	reasoning := &ReasoningConfig{Enabled: false, Effort: "high", BudgetTokens: 2048, Summary: true}
	req := &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "solve"}}, Reasoning: reasoning}
	if body := buildOpenAIRequestBody(req, "gpt-test", false); body["reasoning_effort"] != nil {
		t.Fatalf("disabled OpenAI reasoning leaked into request: %#v", body)
	}
	if body := NewAnthropicWithConfig(ProviderConfig{Model: "claude-test"}).buildRequestBody(req, false); body["thinking"] != nil {
		t.Fatalf("disabled Anthropic reasoning leaked into request: %#v", body)
	}
	geminiBody := NewGeminiWithConfig(ProviderConfig{Model: "gemini-test"}).buildRequestBody(req)
	if generation, ok := geminiBody["generationConfig"].(map[string]any); ok && generation["thinkingConfig"] != nil {
		t.Fatalf("disabled Gemini reasoning leaked into request: %#v", geminiBody)
	}
}

func TestAnthropicThinkingRequest(t *testing.T) {
	provider := NewAnthropicWithConfig(ProviderConfig{Model: "claude-test"})
	body := provider.buildRequestBody(&ChatRequest{
		Messages:  []Message{{Role: RoleUser, Content: "solve"}},
		Reasoning: &ReasoningConfig{Enabled: true, BudgetTokens: 2048},
	}, false)
	thinking, ok := body["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking config missing: %#v", body)
	}
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != 2048 {
		t.Fatalf("thinking = %#v", thinking)
	}
	if maxTokens, _ := body["max_tokens"].(int); maxTokens <= 2048 {
		t.Fatalf("max_tokens = %d, must exceed thinking budget", maxTokens)
	}
}

func TestGeminiThinkingRequest(t *testing.T) {
	provider := NewGeminiWithConfig(ProviderConfig{Model: "gemini-test"})
	body := provider.buildRequestBody(&ChatRequest{
		Messages:  []Message{{Role: RoleUser, Content: "solve"}},
		Reasoning: &ReasoningConfig{Enabled: true, BudgetTokens: 1024, Summary: true},
	})
	generation, ok := body["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("generationConfig missing: %#v", body)
	}
	thinking, ok := generation["thinkingConfig"].(map[string]any)
	if !ok {
		t.Fatalf("thinkingConfig missing: %#v", generation)
	}
	if thinking["thinkingBudget"] != 1024 || thinking["includeThoughts"] != true {
		t.Fatalf("thinkingConfig = %#v", thinking)
	}
}

func TestAnthropicReasoningResponse(t *testing.T) {
	var raw anthropicResponse
	if err := json.Unmarshal([]byte(`{
		"id":"msg_reasoning",
		"content":[
			{"type":"thinking","thinking":"checked the alternatives"},
			{"type":"text","text":"final answer"}
		],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":3,"output_tokens":5}
	}`), &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	resp := NewAnthropic("test").convertResponse(&raw)
	if resp.Reasoning != "checked the alternatives" || resp.Content != "final answer" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestSignedReasoningProvidersRejectTools(t *testing.T) {
	req := &ChatRequest{
		Messages:  []Message{{Role: RoleUser, Content: "solve"}},
		Reasoning: &ReasoningConfig{Enabled: true, BudgetTokens: 1024},
		Tools: []ToolDefinition{{
			Type:     "function",
			Function: FunctionDef{Name: "lookup"},
		}},
	}
	providers := []Provider{
		NewAnthropicWithConfig(ProviderConfig{Model: "claude-test"}),
		NewGeminiWithConfig(ProviderConfig{Model: "gemini-test"}),
	}
	for _, provider := range providers {
		t.Run(provider.Name(), func(t *testing.T) {
			if _, err := provider.Chat(t.Context(), req); err == nil || !strings.Contains(err.Error(), "signed thinking blocks") {
				t.Fatalf("Chat error = %v", err)
			}
			if _, err := provider.StreamChat(t.Context(), req); err == nil || !strings.Contains(err.Error(), "signed thinking blocks") {
				t.Fatalf("StreamChat error = %v", err)
			}
		})
	}
}

func TestAggregateStreamReasoning(t *testing.T) {
	ch := make(chan *ChatResponse, 2)
	ch <- &ChatResponse{Reasoning: "consider ", Delta: true}
	ch <- &ChatResponse{Reasoning: "alternatives", Content: "answer", Delta: true}
	close(ch)
	resp, err := AggregateStream(t.Context(), ch)
	if err != nil {
		t.Fatalf("AggregateStream: %v", err)
	}
	if resp.Reasoning != "consider alternatives" || resp.Content != "answer" {
		t.Fatalf("response = %#v", resp)
	}
}
