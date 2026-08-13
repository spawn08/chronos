package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildResponsesRequestBodyPreservesReasoningToolState(t *testing.T) {
	state := []json.RawMessage{
		json.RawMessage(`{"id":"rs_1","type":"reasoning","encrypted_content":"opaque"}`),
		json.RawMessage(`{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"}`),
	}
	req := &ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "be helpful"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Name: "lookup", Arguments: `{"q":"x"}`}}, ProviderState: state},
			{Role: RoleTool, ToolCallID: "call_1", Content: `{"result":"y"}`},
		},
		Tools:     []ToolDefinition{{Type: "function", Function: FunctionDef{Name: "lookup", Description: "look up", Parameters: map[string]any{"type": "object"}}}},
		Reasoning: &ReasoningConfig{Enabled: true, Effort: "high", Summary: true},
	}

	body := buildResponsesRequestBody(req, "deployment", true)
	if body["model"] != "deployment" || body["stream"] != true || body["store"] != false {
		t.Fatalf("unexpected base body: %#v", body)
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning = %#v", body["reasoning"])
	}
	tools, ok := body["tools"].([]map[string]any)
	if !ok || len(tools) != 1 || tools[0]["name"] != "lookup" || tools[0]["function"] != nil {
		t.Fatalf("responses tools = %#v", body["tools"])
	}
	input, ok := body["input"].([]any)
	if !ok || len(input) != 4 {
		t.Fatalf("input = %#v", body["input"])
	}
	if raw, stateOK := input[1].(json.RawMessage); !stateOK || !strings.Contains(string(raw), `"type":"reasoning"`) {
		t.Fatalf("encrypted reasoning state was not preserved: %#v", input[1])
	}
	output, ok := input[3].(map[string]any)
	if !ok || output["type"] != "function_call_output" || output["call_id"] != "call_1" {
		t.Fatalf("tool output = %#v", input[3])
	}
}

func TestConvertResponsesResponse(t *testing.T) {
	raw := &responsesAPIResponse{
		ID:     "resp_1",
		Status: "completed",
		Output: []json.RawMessage{
			json.RawMessage(`{"id":"rs_1","type":"reasoning","summary":[{"type":"summary_text","text":"checked the plan"}],"encrypted_content":"opaque"}`),
			json.RawMessage(`{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"}`),
		},
	}
	raw.Usage.InputTokens = 7
	raw.Usage.OutputTokens = 11

	resp := convertResponsesResponse(raw)
	if resp.ID != "resp_1" || resp.Reasoning != "checked the plan" || resp.StopReason != StopReasonToolCall {
		t.Fatalf("response = %#v", resp)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "lookup" {
		t.Fatalf("tool calls = %#v", resp.ToolCalls)
	}
	if resp.ProviderState == nil || resp.Usage.PromptTokens != 7 || resp.Usage.CompletionTokens != 11 {
		t.Fatalf("metadata = %#v", resp)
	}
}

func TestReadResponsesSSEStream(t *testing.T) {
	completed := `{"id":"resp_1","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"answer"}]}],"usage":{"input_tokens":3,"output_tokens":5}}`
	body := strings.Join([]string{
		`data: {"type":"response.reasoning_summary_text.delta","delta":"thought "}`,
		`data: {"type":"response.output_text.delta","delta":"ans"}`,
		`data: {"type":"response.output_text.delta","delta":"wer"}`,
		`data: {"type":"response.completed","response":` + completed + `}`,
		`data: [DONE]`,
	}, "\n\n")
	resp := &http.Response{Body: http.NoBody}
	resp.Body = ioNopCloser{Reader: strings.NewReader(body)}
	ch := make(chan *ChatResponse, 8)
	readResponsesSSEStream(context.Background(), resp, ch)
	close(ch)

	agg, err := AggregateStream(context.Background(), ch)
	if err != nil {
		t.Fatalf("AggregateStream: %v", err)
	}
	if agg.Content != "answer" || agg.Reasoning != "thought " || agg.Usage.PromptTokens != 3 || agg.ProviderState == nil {
		t.Fatalf("aggregate = %#v", agg)
	}
}

// ioNopCloser keeps this test self-contained without a second response server.
type ioNopCloser struct{ *strings.Reader }

func (ioNopCloser) Close() error { return nil }

func TestAzureNativeReasoningUsesResponsesEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/v1/responses" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["reasoning"] == nil || body["tools"] == nil {
			t.Errorf("request missing reasoning/tools: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_1","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":1,"output_tokens":2}}`)
	}))
	defer srv.Close()

	p := NewAzureOpenAIWithConfig(AzureConfig{
		ProviderConfig: ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "d"},
		Deployment:     "d",
	})
	resp, err := p.Chat(t.Context(), &ChatRequest{
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		Tools:     []ToolDefinition{{Type: "function", Function: FunctionDef{Name: "lookup"}}},
		Reasoning: &ReasoningConfig{Enabled: true, Effort: "high"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "done" {
		t.Fatalf("content = %q", resp.Content)
	}
}
