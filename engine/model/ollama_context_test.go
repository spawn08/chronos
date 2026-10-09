package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaContextWindow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		parameters string
		trained    float64
		env        string
		status     int
		want       int
		ok         bool
	}{
		{name: "model num_ctx", parameters: "stop \"<|eot|>\"\nnum_ctx 8192", trained: 131072, want: 8192, ok: true},
		{name: "server default", trained: 131072, want: ollamaDefaultContext, ok: true},
		{name: "server env", trained: 131072, env: "32768", want: 32768, ok: true},
		{name: "capped at trained length", trained: 16384, env: "32768", want: 16384, ok: true},
		{name: "show unavailable", status: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OLLAMA_CONTEXT_LENGTH", tc.env)
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/show" {
					t.Errorf("path = %s, want /api/show", r.URL.Path)
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{
					"parameters": tc.parameters,
					"model_info": map[string]any{"llama.context_length": tc.trained, "general.architecture": "llama"},
				})
			}))
			defer srv.Close()
			p := NewOllamaWithConfig(ProviderConfig{BaseURL: srv.URL, Model: "llama3.2"})
			for i := 0; i < 2; i++ {
				got, ok := p.ContextWindow(context.Background())
				if got != tc.want || ok != tc.ok {
					t.Fatalf("ContextWindow() = (%d, %v), want (%d, %v)", got, ok, tc.want, tc.ok)
				}
			}
			if calls != 1 {
				t.Fatalf("/api/show called %d times, want once", calls)
			}
			if limit, ok := ServedContextLimit(context.Background(), p); ok != tc.ok || limit != tc.want {
				t.Fatalf("ServedContextLimit() = (%d, %v)", limit, ok)
			}
		})
	}
}
