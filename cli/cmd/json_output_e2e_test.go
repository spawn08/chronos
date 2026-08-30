package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestExecute_Run_JSONFlagEndToEnd proves --json, placed before the
// subcommand, makes `chronos run` print exactly one JSON object to stdout
// instead of the usual "Agent: ...\nMessage: ...\n<content>" text —
// previously the only command with any JSON output at all was `chronos
// pipe`, which uses a fixed, undocumented-as-general-purpose protocol of its
// own.
func TestExecute_Run_JSONFlagEndToEnd(t *testing.T) {
	chatBody := `{"id":"r1","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello back"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(chatBody))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	yaml := fmt.Sprintf(`agents:
  - id: json-agent
    name: JSONAgent
    model:
      provider: compatible
      model: test-model
      base_url: %q
      api_key: test-key
`, srv.URL)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "json.db"))

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	// --json placed BEFORE "run": only meaningful because it is a global
	// flag, same proof shape as the --stream and --output-schema tests.
	os.Args = []string{"chronos", "--json", "run", "--agent", "json-agent", "hi"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout was not a single JSON object: %v\noutput: %q", err, out)
	}
	if got["agent"] != "json-agent" {
		t.Errorf("agent = %v, want json-agent", got["agent"])
	}
	if got["content"] != "hello back" {
		t.Errorf("content = %v, want %q", got["content"], "hello back")
	}
	usage, ok := got["usage"].(map[string]any)
	if !ok || usage["prompt_tokens"] != float64(3) {
		t.Errorf("usage = %#v, want prompt_tokens=3", got["usage"])
	}
	if got["message"] != "hi" {
		t.Errorf("message = %v, want hi", got["message"])
	}
}

// TestExecute_TeamRun_JSONFlagEndToEnd proves --json works the same way for
// `chronos team run`, dumping the team's result state as one JSON object.
func TestExecute_TeamRun_JSONFlagEndToEnd(t *testing.T) {
	body := `{"id":"r","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"step"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	yaml := fmt.Sprintf(`agents:
  - id: ta
    name: A
    model:
      provider: compatible
      model: m
      base_url: %q
      api_key: k
teams:
  - id: solo
    name: Solo
    strategy: sequential
    agents: [ta]
`, srv.URL)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "team-json.db"))

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "--json", "team", "run", "solo", "hello team"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout was not a single JSON object: %v\noutput: %q", err, out)
	}
	if got["team"] != "solo" {
		t.Errorf("team = %v, want solo", got["team"])
	}
	if got["response"] != "step" {
		t.Errorf("response = %v, want step", got["response"])
	}
}
