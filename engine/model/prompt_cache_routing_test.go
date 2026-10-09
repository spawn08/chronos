package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureChatBody serves one OpenAI-style completion and records its request body.
func captureChatBody(t *testing.T, got *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
}

func TestOpenAISendsPromptCacheKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  ChatRequest
		want any
	}{
		{name: "keyed", req: ChatRequest{CacheKey: "chronos-abc"}, want: "chronos-abc"},
		{name: "no key", req: ChatRequest{}, want: nil},
		{name: "cache disabled", req: ChatRequest{CacheKey: "chronos-abc", DisablePromptCache: true}, want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]any
			srv := captureChatBody(t, &body)
			defer srv.Close()
			p := NewOpenAIWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "gpt-5.5"})
			tc.req.Messages = []Message{{Role: RoleUser, Content: "hi"}}
			if _, err := p.Chat(context.Background(), &tc.req); err != nil {
				t.Fatal(err)
			}
			if body["prompt_cache_key"] != tc.want {
				t.Fatalf("prompt_cache_key = %#v, want %#v", body["prompt_cache_key"], tc.want)
			}
		})
	}
}

// OpenAI-compatible servers may reject unknown parameters; only OpenRouter
// gets a cache hint, and only for Claude, which caches nothing unasked.
func TestCompatibleProvidersPromptCacheHints(t *testing.T) {
	for _, tc := range []struct {
		provider, model string
		wantCacheCtl    bool
	}{
		{provider: "openrouter", model: "anthropic/claude-sonnet-5-5", wantCacheCtl: true},
		{provider: "openrouter", model: "openai/gpt-5.5"},
		{provider: "groq", model: "llama-3.3-70b"},
		{provider: "deepseek", model: "deepseek-chat"},
	} {
		t.Run(tc.provider+"/"+tc.model, func(t *testing.T) {
			var body map[string]any
			srv := captureChatBody(t, &body)
			defer srv.Close()
			p := NewOpenAICompatible(tc.provider, srv.URL, "k", tc.model)
			req := &ChatRequest{CacheKey: "chronos-abc", Messages: []Message{{Role: RoleUser, Content: "hi"}}}
			if _, err := p.Chat(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if _, ok := body["prompt_cache_key"]; ok {
				t.Fatalf("%s received prompt_cache_key", tc.provider)
			}
			cc, ok := body["cache_control"].(map[string]any)
			if ok != tc.wantCacheCtl || (ok && cc["type"] != "ephemeral") {
				t.Fatalf("cache_control = %#v, want present=%v", body["cache_control"], tc.wantCacheCtl)
			}
		})
	}
}
