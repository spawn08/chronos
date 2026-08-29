package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/storage"
)

func TestBuildServeGraphOptions(t *testing.T) {
	t.Run("no config found returns no options, no error", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("CHRONOS_CONFIG", filepath.Join(tmp, "missing.yaml"))
		opts, err := buildServeGraphOptions()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opts) != 0 {
			t.Errorf("expected no options, got %d", len(opts))
		}
	})

	t.Run("non-durable agent with a graph is not registered", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "agents.yaml")
		yaml := `agents:
  - id: quiet-agent
    name: Quiet
    model:
      provider: ollama
    graph:
      entry: n
      nodes:
        - id: n
          type: passthrough
`
		if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CHRONOS_CONFIG", cfgPath)
		opts, err := buildServeGraphOptions()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opts) != 0 {
			t.Errorf("expected no options for a non-durable agent, got %d", len(opts))
		}
	})

	t.Run("durable agent with a graph is registered", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "agents.yaml")
		dbPath := filepath.Join(tmp, "durable.db")
		yaml := fmt.Sprintf(`agents:
  - id: loud-agent
    name: Loud
    model:
      provider: ollama
    durable: true
    storage:
      backend: sqlite
      dsn: %q
    graph:
      entry: n
      nodes:
        - id: n
          type: passthrough
`, dbPath)
		if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CHRONOS_CONFIG", cfgPath)
		opts, err := buildServeGraphOptions()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opts) != 1 {
			t.Fatalf("expected exactly one WithGraphs option, got %d", len(opts))
		}
	})

	t.Run("invalid config surfaces a build error", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "agents.yaml")
		yaml := `agents:
  - id: bad-agent
    name: Bad
    model:
      provider: azure
`
		if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CHRONOS_CONFIG", cfgPath)
		if _, err := buildServeGraphOptions(); err == nil {
			t.Fatal("expected an error for an azure agent missing endpoint/deployment")
		}
	})

	t.Run("malformed YAML surfaces an error rather than being swallowed like no-config", func(t *testing.T) {
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, "agents.yaml")
		// KnownFields(true) rejects the unrecognized "bogus_field" — this is a
		// parse-time error, distinct from ErrConfigNotFound, and must fail
		// buildServeGraphOptions rather than being treated as "no config found".
		yaml := `agents:
  - id: bad-agent
    name: Bad
    bogus_field: true
    model:
      provider: ollama
`
		if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CHRONOS_CONFIG", cfgPath)
		if _, err := buildServeGraphOptions(); err == nil {
			t.Fatal("expected an error for malformed YAML, not a silent no-op")
		}
	})

}

// fakeClosingStorage is a minimal storage.Storage double that only tracks
// whether Close was called — every other method panics, since
// closeAgentStorage must never call anything else.
type fakeClosingStorage struct {
	storage.Storage
	closed bool
}

func (f *fakeClosingStorage) Close() error {
	f.closed = true
	return nil
}

func TestCloseAgentStorage(t *testing.T) {
	withStorage := &fakeClosingStorage{}
	agents := map[string]*agent.Agent{
		"has-storage": {Storage: withStorage},
		"no-storage":  {Storage: nil},
	}
	closeAgentStorage(agents)
	if !withStorage.closed {
		t.Error("expected the agent's storage.Storage to be closed")
	}
	// agents["no-storage"] having a nil Storage must not panic — asserted
	// simply by closeAgentStorage above having returned normally.
}
