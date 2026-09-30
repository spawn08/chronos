package model

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

const maxTokensUnsupported = `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead.","type":"invalid_request_error","param":"max_tokens","code":"unsupported_parameter"}}`

// maxTokensServer emulates a model that rejects "max_tokens" and records the
// token-cap parameter each request used.
func maxTokensServer(t *testing.T, streaming bool) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		switch {
		case body["max_tokens"] != nil:
			seen = append(seen, "max_tokens")
		case body["max_completion_tokens"] != nil:
			seen = append(seen, "max_completion_tokens")
		default:
			seen = append(seen, "none")
		}
		mu.Unlock()
		if body["max_tokens"] != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, maxTokensUnsupported)
			return
		}
		if v, _ := body["max_completion_tokens"].(float64); v != 256 {
			t.Errorf("max_completion_tokens = %v, want 256", body["max_completion_tokens"])
		}
		if streaming {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"c1\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, `{"id":"c1","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestMaxCompletionTokensFallback(t *testing.T) {
	providers := map[string]func(baseURL string) Provider{
		"azure-classic": func(u string) Provider {
			return NewAzureOpenAIWithConfig(AzureConfig{
				ProviderConfig: ProviderConfig{APIKey: "k", BaseURL: u},
				Deployment:     "gpt-6.1-sol", APIVersion: "2024-12-01-preview",
			})
		},
		"azure-v1": func(u string) Provider {
			return NewAzureOpenAIWithConfig(AzureConfig{
				ProviderConfig: ProviderConfig{APIKey: "k", BaseURL: u},
				Deployment:     "gpt-6.1-sol", APIVersion: "preview",
			})
		},
		"openai": func(u string) Provider {
			return NewOpenAIWithConfig(ProviderConfig{APIKey: "k", BaseURL: u, Model: "gpt-6.1-sol"})
		},
	}
	for name, newProvider := range providers {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", name, streaming), func(t *testing.T) {
				srv, seen := maxTokensServer(t, streaming)
				defer srv.Close()
				p := newProvider(srv.URL)

				call := func() {
					t.Helper()
					req := &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}, MaxTokens: 256}
					var resp *ChatResponse
					var err error
					if streaming {
						ch, streamErr := p.StreamChat(t.Context(), req)
						if streamErr != nil {
							t.Fatalf("StreamChat: %v", streamErr)
						}
						resp, err = AggregateStream(t.Context(), ch)
					} else {
						resp, err = p.Chat(t.Context(), req)
					}
					if err != nil || resp.Content != "ok" {
						t.Fatalf("response = %+v, err = %v", resp, err)
					}
				}

				call()
				call() // the preference is remembered: no second rejected request
				got := seen()
				want := []string{"max_tokens", "max_completion_tokens", "max_completion_tokens"}
				if fmt.Sprint(got) != fmt.Sprint(want) {
					t.Fatalf("token params = %v, want %v", got, want)
				}
			})
		}
	}
}

func TestMaxCompletionTokensRetriesOnlyOnce(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, maxTokensUnsupported)
	}))
	defer srv.Close()
	p := NewAzureOpenAIWithConfig(AzureConfig{
		ProviderConfig: ProviderConfig{APIKey: "k", BaseURL: srv.URL},
		Deployment:     "d", APIVersion: "preview",
	})
	_, err := p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}, MaxTokens: 10})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (original + one retry)", calls)
	}
}

func TestMaxTokensParamRejected(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"openai unsupported", http.StatusBadRequest, maxTokensUnsupported, true},
		{"other 400", http.StatusBadRequest, `{"error":{"message":"Invalid temperature"}}`, false},
		{"wrong status", http.StatusUnauthorized, maxTokensUnsupported, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maxTokensParamRejected(tt.status, tt.body); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
