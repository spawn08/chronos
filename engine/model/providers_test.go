package model

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func openAISuccessBody(content string) string {
	return fmt.Sprintf(`{"id":"r1","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":%q}}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`, content)
}

func buildTestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

func sseBody(chunks ...string) string {
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString(fmt.Sprintf(`data: {"id":"s1","choices":[{"delta":{"content":%q}}]}`+"\n", c))
	}
	sb.WriteString("data: [DONE]\n")
	return sb.String()
}

// ---------------------------------------------------------------------------
// Azure OpenAI tests
// ---------------------------------------------------------------------------

func TestAzureOpenAI_NewAzureOpenAI(t *testing.T) {
	p := NewAzureOpenAI("https://res.openai.azure.com", "key", "gpt4-deploy")
	if p.Name() != "azure-openai" {
		t.Errorf("Name=%q", p.Name())
	}
	if p.Model() != "gpt4-deploy" {
		t.Errorf("Model=%q", p.Model())
	}
}

func TestAzureOpenAI_NewAzureOpenAIWithConfig_DefaultVersion(t *testing.T) {
	p := NewAzureOpenAIWithConfig(AzureConfig{
		ProviderConfig: ProviderConfig{APIKey: "k", BaseURL: "https://x.openai.azure.com", Model: "d"},
		Deployment:     "d",
	})
	if p.apiVersion != "2024-10-21" {
		t.Errorf("default api version: got %q", p.apiVersion)
	}
}

func TestAzureOpenAI_Chat_Success(t *testing.T) {
	srv := buildTestServer(t, 200, openAISuccessBody("azure response"))
	defer srv.Close()

	p := NewAzureOpenAIWithConfig(AzureConfig{
		ProviderConfig: ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "d"},
		Deployment:     "d",
		APIVersion:     "2024-10-21",
	})
	resp, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "azure response" {
		t.Errorf("Content=%q", resp.Content)
	}
}

func TestAzureOpenAI_Chat_Error(t *testing.T) {
	srv := buildTestServer(t, 401, `{"error":"unauthorized"}`)
	defer srv.Close()

	p := NewAzureOpenAIWithConfig(AzureConfig{
		ProviderConfig: ProviderConfig{APIKey: "bad", BaseURL: srv.URL, Model: "d"},
		Deployment:     "d",
		APIVersion:     "2024-10-21",
	})
	_, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "azure openai chat") {
		t.Errorf("error=%v", err)
	}
}

func TestAzureOpenAI_StreamChat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody("Hello", " Azure"))
	}))
	defer srv.Close()

	p := NewAzureOpenAIWithConfig(AzureConfig{
		ProviderConfig: ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "d"},
		Deployment:     "d",
		APIVersion:     "2024-10-21",
	})
	ch, err := p.StreamChat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var buf strings.Builder
	for r := range ch {
		buf.WriteString(r.Content)
	}
	if buf.String() != "Hello Azure" {
		t.Errorf("stream=%q", buf.String())
	}
}

func TestAzureOpenAI_StreamChat_Error(t *testing.T) {
	srv := buildTestServer(t, 500, `{"error":"server error"}`)
	defer srv.Close()

	p := NewAzureOpenAIWithConfig(AzureConfig{
		ProviderConfig: ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "d"},
		Deployment:     "d",
		APIVersion:     "2024-10-21",
	})
	_, err := p.StreamChat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAzureOpenAI_chatPath(t *testing.T) {
	p := NewAzureOpenAI("https://x", "k", "my-deploy")
	path := p.chatPath()
	if !strings.Contains(path, "my-deploy") {
		t.Errorf("chatPath=%q, want my-deploy", path)
	}
	if !strings.Contains(path, "2024-10-21") {
		t.Errorf("chatPath=%q, want api-version", path)
	}
}

// ---------------------------------------------------------------------------
// Azure Embeddings tests
// ---------------------------------------------------------------------------

func buildEmbeddingsServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

var embeddingSuccessBody = `{"data":[{"embedding":[0.1,0.2,0.3],"index":0}],"usage":{"prompt_tokens":5,"total_tokens":5}}`

func TestAzureOpenAIEmbeddings_NewAzureOpenAIEmbeddings(t *testing.T) {
	p := NewAzureOpenAIEmbeddings("https://res.openai.azure.com", "key", "embed-deploy")
	if p.deployment != "embed-deploy" {
		t.Errorf("deployment=%q", p.deployment)
	}
}

func TestAzureOpenAIEmbeddings_Embed_Success(t *testing.T) {
	srv := buildEmbeddingsServer(t, 200, embeddingSuccessBody)
	defer srv.Close()

	p := NewAzureOpenAIEmbeddingsWithConfig(
		ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "d"},
		"d", "2024-02-01",
	)
	resp, err := p.Embed(t.Context(), &EmbeddingRequest{Model: "d", Input: []string{"hello"}})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Embeddings) != 1 {
		t.Errorf("embeddings count=%d", len(resp.Embeddings))
	}
	if resp.Embeddings[0][0] != 0.1 {
		t.Errorf("embeddings[0][0]=%f", resp.Embeddings[0][0])
	}
}

func TestAzureOpenAIEmbeddings_Embed_Error(t *testing.T) {
	srv := buildEmbeddingsServer(t, 401, `{"error":"unauth"}`)
	defer srv.Close()

	p := NewAzureOpenAIEmbeddingsWithConfig(
		ProviderConfig{APIKey: "bad", BaseURL: srv.URL, Model: "d"},
		"d", "2024-02-01",
	)
	_, err := p.Embed(t.Context(), &EmbeddingRequest{Input: []string{"text"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAzureOpenAIEmbeddings_NewWithDefaultVersion(t *testing.T) {
	p := NewAzureOpenAIEmbeddingsWithConfig(
		ProviderConfig{APIKey: "k", BaseURL: "https://x", Model: "d"},
		"d", "",
	)
	if p.apiVersion != "2024-02-01" {
		t.Errorf("default version=%q", p.apiVersion)
	}
}

// ---------------------------------------------------------------------------
// Cohere tests
// ---------------------------------------------------------------------------

var cohereSuccessBody = `{"id":"c1","message":{"role":"assistant","content":[{"type":"text","text":"cohere resp"}],"tool_calls":[]},"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":5,"output_tokens":3}}}`

func TestCohere_NewCohere(t *testing.T) {
	p := NewCohere("key", "command-r")
	if p.Name() != "cohere" {
		t.Errorf("Name=%q", p.Name())
	}
	if p.Model() != "command-r" {
		t.Errorf("Model=%q", p.Model())
	}
}

func TestCohere_NewCohereWithConfig_Defaults(t *testing.T) {
	p := NewCohereWithConfig(ProviderConfig{APIKey: "k"})
	if p.config.BaseURL != "https://api.cohere.ai" {
		t.Errorf("BaseURL=%q", p.config.BaseURL)
	}
	if p.config.Model != "command-r-plus" {
		t.Errorf("Model=%q", p.config.Model)
	}
}

func TestCohere_Chat_Success(t *testing.T) {
	srv := buildTestServer(t, 200, cohereSuccessBody)
	defer srv.Close()

	p := NewCohereWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "command-r"})
	resp, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "cohere resp" {
		t.Errorf("Content=%q", resp.Content)
	}
	if resp.StopReason != StopReasonEnd {
		t.Errorf("StopReason=%q", resp.StopReason)
	}
	if resp.Usage.PromptTokens != 5 {
		t.Errorf("PromptTokens=%d", resp.Usage.PromptTokens)
	}
}

func TestCohere_Chat_Error(t *testing.T) {
	srv := buildTestServer(t, 400, `{"message":"bad request"}`)
	defer srv.Close()

	p := NewCohereWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "command-r"})
	_, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cohere chat") {
		t.Errorf("error=%v", err)
	}
}

func TestCohere_Chat_InvalidJSON(t *testing.T) {
	srv := buildTestServer(t, 200, "not-json")
	defer srv.Close()

	p := NewCohereWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "command-r"})
	_, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCohere_Chat_MaxTokensFinishReason(t *testing.T) {
	body := `{"id":"c2","message":{"role":"assistant","content":[{"type":"text","text":"cut"}]},"finish_reason":"MAX_TOKENS","usage":{"tokens":{}}}`
	srv := buildTestServer(t, 200, body)
	defer srv.Close()

	p := NewCohereWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "command-r"})
	resp, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.StopReason != StopReasonMaxTokens {
		t.Errorf("StopReason=%q", resp.StopReason)
	}
}

func TestCohere_Chat_ToolCallFinishReason(t *testing.T) {
	body := `{"id":"c3","message":{"role":"assistant","content":[],"tool_calls":[{"id":"t1","type":"function","function":{"name":"search","arguments":"{}"}}]},"finish_reason":"TOOL_CALL","usage":{"tokens":{}}}`
	srv := buildTestServer(t, 200, body)
	defer srv.Close()

	p := NewCohereWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "command-r"})
	resp, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.StopReason != StopReasonToolCall {
		t.Errorf("StopReason=%q, want tool_call", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCalls=%+v", resp.ToolCalls)
	}
}

func TestCohere_StreamChat_Success(t *testing.T) {
	streamBody := `data: {"type":"content-delta","delta":{"message":{"content":{"text":"hello"}}}}
data: {"type":"content-delta","delta":{"message":{"content":{"text":" cohere"}}}}
data: [DONE]
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, streamBody)
	}))
	defer srv.Close()

	p := NewCohereWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "command-r"})
	ch, err := p.StreamChat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var buf strings.Builder
	for r := range ch {
		buf.WriteString(r.Content)
	}
	if buf.String() != "hello cohere" {
		t.Errorf("stream=%q", buf.String())
	}
}

func TestCohere_StreamChat_Error(t *testing.T) {
	srv := buildTestServer(t, 500, `{"error":"server"}`)
	defer srv.Close()

	p := NewCohereWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "command-r"})
	_, err := p.StreamChat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCohere_buildRequestBody_WithOptions(t *testing.T) {
	p := NewCohereWithConfig(ProviderConfig{APIKey: "k", Model: "command-r"})
	req := &ChatRequest{
		Messages:    []Message{{Role: RoleSystem, Content: "sys"}, {Role: RoleUser, Content: "user"}},
		MaxTokens:   100,
		Temperature: 0.5,
		Tools: []ToolDefinition{
			{Type: "function", Function: FunctionDef{Name: "fn", Description: "a func"}},
		},
	}
	body := p.buildRequestBody(req, false)
	if body["max_tokens"] != 100 {
		t.Errorf("max_tokens=%v", body["max_tokens"])
	}
	if body["temperature"] != 0.5 {
		t.Errorf("temperature=%v", body["temperature"])
	}
	if body["tools"] == nil {
		t.Error("tools should be set")
	}
}

// ---------------------------------------------------------------------------
// Mistral tests
// ---------------------------------------------------------------------------

func TestMistral_NewMistral(t *testing.T) {
	p := NewMistral("key")
	if p.Name() != "mistral" {
		t.Errorf("Name=%q", p.Name())
	}
	if p.Model() != "mistral-large-latest" {
		t.Errorf("Model=%q", p.Model())
	}
}

func TestMistral_NewMistralWithConfig_Defaults(t *testing.T) {
	p := NewMistralWithConfig(ProviderConfig{APIKey: "k"})
	if p.config.BaseURL != "https://api.mistral.ai/v1" {
		t.Errorf("BaseURL=%q", p.config.BaseURL)
	}
	if p.config.Model != "mistral-large-latest" {
		t.Errorf("Model=%q", p.config.Model)
	}
}

func TestMistral_Chat_Success(t *testing.T) {
	srv := buildTestServer(t, 200, openAISuccessBody("mistral response"))
	defer srv.Close()

	p := NewMistralWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "mistral-large-latest"})
	resp, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "mistral response" {
		t.Errorf("Content=%q", resp.Content)
	}
}

func TestMistral_Chat_Error(t *testing.T) {
	srv := buildTestServer(t, 429, `{"error":"rate limited"}`)
	defer srv.Close()

	p := NewMistralWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "m"})
	_, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "mistral chat") {
		t.Errorf("error=%v", err)
	}
}

func TestMistral_Chat_InvalidJSON(t *testing.T) {
	srv := buildTestServer(t, 200, "not-json")
	defer srv.Close()

	p := NewMistralWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "m"})
	_, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMistral_StreamChat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sseBody("Hello", " Mistral"))
	}))
	defer srv.Close()

	p := NewMistralWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "m"})
	ch, err := p.StreamChat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var buf strings.Builder
	for r := range ch {
		buf.WriteString(r.Content)
	}
	if buf.String() != "Hello Mistral" {
		t.Errorf("stream=%q", buf.String())
	}
}

func TestMistral_StreamChat_Error(t *testing.T) {
	srv := buildTestServer(t, 503, `{"error":"unavailable"}`)
	defer srv.Close()

	p := NewMistralWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "m"})
	_, err := p.StreamChat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// Ollama tests
// ---------------------------------------------------------------------------

func TestOllama_NewOllama(t *testing.T) {
	p := NewOllama("http://localhost:11434", "llama3.2")
	if p.Name() != "ollama" {
		t.Errorf("Name=%q", p.Name())
	}
	if p.Model() != "llama3.2" {
		t.Errorf("Model=%q", p.Model())
	}
}

func TestOllama_NewOllamaWithConfig_Defaults(t *testing.T) {
	p := NewOllamaWithConfig(ProviderConfig{})
	if p.config.BaseURL != "http://localhost:11434" {
		t.Errorf("BaseURL=%q", p.config.BaseURL)
	}
	if p.config.Model != "llama3.2" {
		t.Errorf("Model=%q", p.config.Model)
	}
}

func TestOllama_Chat_Success(t *testing.T) {
	srv := buildTestServer(t, 200, openAISuccessBody("ollama response"))
	defer srv.Close()

	p := NewOllamaWithConfig(ProviderConfig{BaseURL: srv.URL, Model: "llama3.2"})
	resp, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "ollama response" {
		t.Errorf("Content=%q", resp.Content)
	}
}

func TestOllama_Chat_Error(t *testing.T) {
	srv := buildTestServer(t, 500, `{"error":"model not found"}`)
	defer srv.Close()

	p := NewOllamaWithConfig(ProviderConfig{BaseURL: srv.URL, Model: "m"})
	_, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ollama chat") {
		t.Errorf("error=%v", err)
	}
}

func TestOllama_Chat_InvalidJSON(t *testing.T) {
	srv := buildTestServer(t, 200, "not-json")
	defer srv.Close()

	p := NewOllamaWithConfig(ProviderConfig{BaseURL: srv.URL, Model: "m"})
	_, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOllama_StreamChat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sseBody("Ollama", " stream"))
	}))
	defer srv.Close()

	p := NewOllamaWithConfig(ProviderConfig{BaseURL: srv.URL, Model: "m"})
	ch, err := p.StreamChat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var buf strings.Builder
	for r := range ch {
		buf.WriteString(r.Content)
	}
	if buf.String() != "Ollama stream" {
		t.Errorf("stream=%q", buf.String())
	}
}

func TestOllama_StreamChat_Error(t *testing.T) {
	srv := buildTestServer(t, 404, `{"error":"not found"}`)
	defer srv.Close()

	p := NewOllamaWithConfig(ProviderConfig{BaseURL: srv.URL, Model: "m"})
	_, err := p.StreamChat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// OpenAI Compatible tests
// ---------------------------------------------------------------------------

func TestOpenAICompatible_New(t *testing.T) {
	p := NewOpenAICompatible("vllm", "http://localhost:8000", "key", "llama-70b")
	if p.Name() != "vllm" {
		t.Errorf("Name=%q", p.Name())
	}
	if p.Model() != "llama-70b" {
		t.Errorf("Model=%q", p.Model())
	}
}

func TestOpenAICompatible_NewWithConfig_NoKey(t *testing.T) {
	p := NewOpenAICompatibleWithConfig("local", ProviderConfig{BaseURL: "http://localhost:8000", Model: "m"})
	if p.Name() != "local" {
		t.Errorf("Name=%q", p.Name())
	}
}

func TestOpenAICompatible_Chat_Success(t *testing.T) {
	srv := buildTestServer(t, 200, openAISuccessBody("compatible response"))
	defer srv.Close()

	p := NewOpenAICompatible("test", srv.URL, "key", "model-x")
	resp, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "compatible response" {
		t.Errorf("Content=%q", resp.Content)
	}
}

func TestOpenAICompatible_Chat_Error(t *testing.T) {
	srv := buildTestServer(t, 400, `{"error":"bad"}`)
	defer srv.Close()

	p := NewOpenAICompatible("test", srv.URL, "k", "m")
	_, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "test chat") {
		t.Errorf("error=%v", err)
	}
}

func TestOpenAICompatible_Chat_InvalidJSON(t *testing.T) {
	srv := buildTestServer(t, 200, "bad")
	defer srv.Close()

	p := NewOpenAICompatible("test", srv.URL, "k", "m")
	_, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenAICompatible_StreamChat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sseBody("Hello", " Compatible"))
	}))
	defer srv.Close()

	p := NewOpenAICompatible("test", srv.URL, "k", "m")
	ch, err := p.StreamChat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var buf strings.Builder
	for r := range ch {
		buf.WriteString(r.Content)
	}
	if buf.String() != "Hello Compatible" {
		t.Errorf("stream=%q", buf.String())
	}
}

func TestOpenAICompatible_StreamChat_Error(t *testing.T) {
	srv := buildTestServer(t, 500, `{"error":"err"}`)
	defer srv.Close()

	p := NewOpenAICompatible("test", srv.URL, "k", "m")
	_, err := p.StreamChat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenAICompatible_ConvenienceConstructors(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		wantName string
	}{
		{"together", NewTogether("k", "llama"), "together"},
		{"groq", NewGroq("k", "llama"), "groq"},
		{"deepseek", NewDeepSeek("k", "deepseek-chat"), "deepseek"},
		{"openrouter", NewOpenRouter("k", "llama"), "openrouter"},
		{"fireworks", NewFireworks("k", "llama"), "fireworks"},
		{"perplexity", NewPerplexity("k", "llama"), "perplexity"},
		{"anyscale", NewAnyscale("k", "llama"), "anyscale"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.provider.Name() != tt.wantName {
				t.Errorf("Name()=%q, want %q", tt.provider.Name(), tt.wantName)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FallbackProvider tests
// ---------------------------------------------------------------------------

type failProvider struct {
	name string
	err  error
}

func (f *failProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return nil, f.err
}
func (f *failProvider) StreamChat(_ context.Context, _ *ChatRequest) (<-chan *ChatResponse, error) {
	return nil, f.err
}
func (f *failProvider) Name() string  { return f.name }
func (f *failProvider) Model() string { return "m" }

type succeedProvider struct {
	response string
}

func (s *succeedProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{Content: s.response}, nil
}
func (s *succeedProvider) StreamChat(_ context.Context, _ *ChatRequest) (<-chan *ChatResponse, error) {
	ch := make(chan *ChatResponse, 1)
	ch <- &ChatResponse{Content: s.response}
	close(ch)
	return ch, nil
}
func (s *succeedProvider) Name() string  { return "succeed" }
func (s *succeedProvider) Model() string { return "m" }

func TestFallbackProvider_NewFallbackProvider_NoProviders(t *testing.T) {
	_, err := NewFallbackProvider()
	if err == nil {
		t.Fatal("expected error for empty providers")
	}
}

func TestFallbackProvider_NewFallbackProvider_Single(t *testing.T) {
	p, err := NewFallbackProvider(&succeedProvider{response: "ok"})
	if err != nil {
		t.Fatalf("NewFallbackProvider: %v", err)
	}
	if p.Name() != "fallback(succeed)" {
		t.Errorf("Name=%q", p.Name())
	}
	if p.Model() != "m" {
		t.Errorf("Model=%q", p.Model())
	}
}

func TestFallbackProvider_Chat_FirstSucceeds(t *testing.T) {
	p, _ := NewFallbackProvider(&succeedProvider{response: "first"}, &succeedProvider{response: "second"})
	resp, err := p.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "first" {
		t.Errorf("Content=%q, want first", resp.Content)
	}
}

func TestFallbackProvider_Chat_FallsToSecond(t *testing.T) {
	var fallbackCalled bool
	p, _ := NewFallbackProvider(
		&failProvider{name: "p1", err: errors.New("p1 failed")},
		&succeedProvider{response: "second"},
	)
	p.OnFallback = func(idx int, name string, err error) {
		fallbackCalled = true
	}
	resp, err := p.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "second" {
		t.Errorf("Content=%q, want second", resp.Content)
	}
	if !fallbackCalled {
		t.Error("OnFallback should have been called")
	}
}

func TestFallbackProvider_Chat_AllFail(t *testing.T) {
	p, _ := NewFallbackProvider(
		&failProvider{name: "p1", err: errors.New("fail1")},
		&failProvider{name: "p2", err: errors.New("fail2")},
	)
	_, err := p.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if !strings.Contains(err.Error(), "all 2 providers failed") {
		t.Errorf("error=%v", err)
	}
}

func TestFallbackProvider_StreamChat_FallsToSecond(t *testing.T) {
	p, _ := NewFallbackProvider(
		&failProvider{name: "p1", err: errors.New("stream fail")},
		&succeedProvider{response: "stream second"},
	)
	ch, err := p.StreamChat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var resp *ChatResponse
	for r := range ch {
		resp = r
	}
	if resp == nil || resp.Content != "stream second" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestFallbackProvider_StreamChat_AllFail(t *testing.T) {
	p, _ := NewFallbackProvider(
		&failProvider{name: "p1", err: errors.New("fail")},
		&failProvider{name: "p2", err: errors.New("fail")},
	)
	_, err := p.StreamChat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFallbackProvider_Chat_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, _ := NewFallbackProvider(
		&failProvider{name: "p1", err: errors.New("fail")},
	)
	_, err := p.Chat(ctx, &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// CachedEmbeddings tests
// ---------------------------------------------------------------------------

type mockEmbeddingsProvider struct {
	calls int
}

func (m *mockEmbeddingsProvider) Embed(_ context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	m.calls++
	embeddings := make([][]float32, len(req.Input))
	for i := range req.Input {
		embeddings[i] = []float32{float32(i + 1), 0.5}
	}
	return &EmbeddingResponse{
		Embeddings: embeddings,
		Usage:      Usage{PromptTokens: len(req.Input) * 5},
	}, nil
}

func TestCachedEmbeddings_CachesResults(t *testing.T) {
	inner := &mockEmbeddingsProvider{}
	cached := NewCachedEmbeddings(inner)
	ctx := context.Background()

	req := &EmbeddingRequest{Model: "m", Input: []string{"hello", "world"}}
	resp1, err := cached.Embed(ctx, req)
	if err != nil {
		t.Fatalf("Embed 1: %v", err)
	}
	if len(resp1.Embeddings) != 2 {
		t.Errorf("embeddings count=%d", len(resp1.Embeddings))
	}
	if inner.calls != 1 {
		t.Errorf("calls=%d, want 1", inner.calls)
	}

	// Second call — should be cached
	resp2, err := cached.Embed(ctx, req)
	if err != nil {
		t.Fatalf("Embed 2: %v", err)
	}
	if inner.calls != 1 {
		t.Errorf("expected cache hit, calls=%d", inner.calls)
	}
	if resp2.Embeddings[0][0] != resp1.Embeddings[0][0] {
		t.Error("cached result differs")
	}
}

func TestCachedEmbeddings_PartialCache(t *testing.T) {
	inner := &mockEmbeddingsProvider{}
	cached := NewCachedEmbeddings(inner)
	ctx := context.Background()

	// Prime cache with "hello"
	_, _ = cached.Embed(ctx, &EmbeddingRequest{Model: "m", Input: []string{"hello"}})

	// Now request "hello" + "new"
	resp, err := cached.Embed(ctx, &EmbeddingRequest{Model: "m", Input: []string{"hello", "new"}})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Embeddings) != 2 {
		t.Errorf("count=%d", len(resp.Embeddings))
	}
	if inner.calls != 2 {
		t.Errorf("calls=%d, want 2", inner.calls)
	}
}

// ---------------------------------------------------------------------------
// Tokenizer tests
// ---------------------------------------------------------------------------

func TestEstimatingCounter_CountTokens(t *testing.T) {
	c := NewEstimatingCounter()
	msgs := []Message{
		{Role: RoleUser, Content: "Hello world"},
		{Role: RoleAssistant, Content: "Hi there", Name: "assistant"},
		{Role: RoleTool, Content: "result", ToolCalls: []ToolCall{{Name: "fn", Arguments: "{}"}}},
	}
	count := c.CountTokens(msgs)
	if count <= 0 {
		t.Errorf("expected positive token count, got %d", count)
	}
}

func TestEstimatingCounter_CountString(t *testing.T) {
	c := NewEstimatingCounter()
	if c.CountString("") != 0 {
		t.Error("empty string should be 0")
	}
	count := c.CountString("Hello")
	if count <= 0 {
		t.Errorf("expected positive, got %d", count)
	}
}

func TestEstimatingCounter_ZeroCharsPerToken(t *testing.T) {
	c := &EstimatingCounter{CharsPerToken: 0}
	count := c.CountString("hello")
	if count <= 0 {
		t.Errorf("expected positive with zero CharsPerToken, got %d", count)
	}
}

func TestContextLimit_KnownModel(t *testing.T) {
	limit := ContextLimit("gpt-4o", 0)
	if limit != 128000 {
		t.Errorf("limit=%d, want 128000", limit)
	}
}

func TestContextLimit_UnknownModel_WithFallback(t *testing.T) {
	limit := ContextLimit("unknown-model", 4096)
	if limit != 4096 {
		t.Errorf("limit=%d, want 4096", limit)
	}
}

func TestContextLimit_UnknownModel_NoFallback(t *testing.T) {
	limit := ContextLimit("unknown-model", 0)
	if limit != 8192 {
		t.Errorf("limit=%d, want 8192", limit)
	}
}

// ---------------------------------------------------------------------------
// Provider message helpers
// ---------------------------------------------------------------------------

func TestMessage_AddImageURL(t *testing.T) {
	m := &Message{Role: RoleUser, Content: "look at this"}
	m.AddImageURL("https://example.com/img.png", "image/png")
	if len(m.Parts) != 1 {
		t.Fatalf("parts count=%d", len(m.Parts))
	}
	if m.Parts[0].Type != "image_url" {
		t.Errorf("type=%q", m.Parts[0].Type)
	}
	if m.Parts[0].ImageURL != "https://example.com/img.png" {
		t.Errorf("url=%q", m.Parts[0].ImageURL)
	}
}

func TestMessage_AddFile(t *testing.T) {
	m := &Message{Role: RoleUser, Content: "here is a file"}
	m.AddFile("doc.pdf", "application/pdf", []byte("content"))
	if len(m.Parts) != 1 {
		t.Fatalf("parts count=%d", len(m.Parts))
	}
	if m.Parts[0].Type != "file" {
		t.Errorf("type=%q", m.Parts[0].Type)
	}
	if m.Parts[0].FileName != "doc.pdf" {
		t.Errorf("filename=%q", m.Parts[0].FileName)
	}
}

func TestMessage_AddAudio(t *testing.T) {
	m := &Message{Role: RoleUser, Content: "listen"}
	m.AddAudio([]byte("audiodata"), "wav")
	if len(m.Parts) != 1 {
		t.Fatalf("parts count=%d", len(m.Parts))
	}
	if m.Parts[0].Type != "audio" {
		t.Errorf("type=%q", m.Parts[0].Type)
	}
	if m.Parts[0].MimeType != "audio/wav" {
		t.Errorf("mimeType=%q", m.Parts[0].MimeType)
	}
}

func TestFallbackProvider_Model_Empty(t *testing.T) {
	// With no providers after construction validation, call Model() with a
	// manually constructed empty FallbackProvider
	p := &FallbackProvider{}
	if p.Model() != "" {
		t.Errorf("Model() with no providers should return empty, got %q", p.Model())
	}
}

func TestFallbackProvider_StreamChat_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled

	p, _ := NewFallbackProvider(&failProvider{err: errors.New("fail")})
	_, err := p.StreamChat(ctx, &ChatRequest{})
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}
