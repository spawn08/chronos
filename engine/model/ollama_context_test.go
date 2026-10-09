package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestOllamaContextWindow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		parameters string
		loaded     int // /api/ps context_length for the model; 0 = not loaded
		want       int
	}{
		{name: "model num_ctx", parameters: "stop \"<|eot|>\"\nnum_ctx 8192", loaded: 32768, want: 8192},
		{name: "loaded model context", loaded: 32768, want: 32768},
		// No firm evidence: server defaults are not visible to the client,
		// so nothing is reported and the configured limit applies.
		{name: "no evidence", want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OLLAMA_CONTEXT_LENGTH", "2048") // a client-side value is not evidence
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/show":
					json.NewEncoder(w).Encode(map[string]any{"parameters": tc.parameters, "model_info": map[string]any{"llama.context_length": 131072}})
				case "/api/ps":
					var models []map[string]any
					if tc.loaded > 0 {
						models = append(models, map[string]any{"name": "llama3.2:latest", "model": "llama3.2:latest", "context_length": tc.loaded})
					}
					json.NewEncoder(w).Encode(map[string]any{"models": models})
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			p := NewOllamaWithConfig(ProviderConfig{BaseURL: srv.URL, Model: "llama3.2"})
			got, ok := ServedContextLimit(context.Background(), p)
			if got != tc.want || ok != (tc.want > 0) {
				t.Fatalf("ServedContextLimit() = (%d, %v), want %d", got, ok, tc.want)
			}
		})
	}
}

// A lookup made while the caller's context is already cancelled, or while
// the server is unreachable, must not be remembered as the answer.
func TestOllamaContextWindowSurvivesCancelledCallerAndRetries(t *testing.T) {
	var up atomic.Bool
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !up.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"parameters": "num_ctx 16384"})
	}))
	defer srv.Close()
	p := NewOllamaWithConfig(ProviderConfig{BaseURL: srv.URL, Model: "llama3.2"})

	up.Store(true)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if got, ok := p.ContextWindow(cancelled); !ok || got != 16384 {
		t.Fatalf("lookup with a cancelled caller = (%d, %v), want 16384", got, ok)
	}

	q := NewOllamaWithConfig(ProviderConfig{BaseURL: srv.URL, Model: "llama3.2"})
	up.Store(false)
	if _, ok := q.ContextWindow(context.Background()); ok {
		t.Fatal("reported a window while the server was down")
	}
	before := calls.Load()
	q.ContextWindow(context.Background()) // within the retry interval: no new lookup
	if calls.Load() != before {
		t.Fatal("re-queried the server within the retry interval")
	}
	up.Store(true)
	q.windowChecked = q.windowChecked.Add(-2 * ollamaWindowRetry) // the interval passed
	if got, ok := q.ContextWindow(context.Background()); !ok || got != 16384 {
		t.Fatalf("retry after the server came up = (%d, %v), want 16384", got, ok)
	}
}
