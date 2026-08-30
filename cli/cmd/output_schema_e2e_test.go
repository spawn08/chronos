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

// TestExecute_Run_OutputSchemaFlagEndToEnd proves --output-schema, placed
// before the subcommand, actually reaches the model provider as a
// schema-constrained request — the full pipeline: stripGlobalFlags parses
// the flag position-independently, applyCLIRuntimeOverrides loads and
// applies the schema file to the loaded agent, Agent.Chat wires it into
// ChatRequest.ResponseFormat/Metadata, and the "compatible" provider (sharing
// buildOpenAIRequestBody with OpenAI/Azure/Ollama/Mistral) sends it as the
// real response_format.json_schema parameter.
func TestExecute_Run_OutputSchemaFlagEndToEnd(t *testing.T) {
	var gotBody map[string]any
	chatBody := `{"id":"r1","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"answer\":\"ok\"}"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(chatBody))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	yaml := fmt.Sprintf(`agents:
  - id: schema-agent
    name: SchemaAgent
    model:
      provider: compatible
      model: test-model
      base_url: %q
      api_key: test-key
`, srv.URL)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(tmp, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(`{"type":"object","required":["answer"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "schema.db"))

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	// --output-schema placed BEFORE the "run" subcommand: this only works at
	// all because it's a global flag (see TestStripGlobalOutputSchemaFlag);
	// it previously would have failed with "unknown command: --output-schema"
	// the same way --stream did before that fix.
	os.Args = []string{"chronos", "--output-schema", schemaPath, "run", "--agent", "schema-agent", "ping"}

	if err := Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rf, ok := gotBody["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("response_format = %#v, want map[string]any", gotBody["response_format"])
	}
	if rf["type"] != "json_schema" {
		t.Errorf("response_format.type = %v, want json_schema", rf["type"])
	}
	js, ok := rf["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("response_format.json_schema = %#v, want map[string]any", rf["json_schema"])
	}
	schema, ok := js["schema"].(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Errorf("response_format.json_schema.schema = %#v, want the --output-schema file's contents", js["schema"])
	}
}
