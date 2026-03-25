package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFile_ExplicitMissing_Deep(t *testing.T) {
	_, err := LoadFile("/this/path/does/not/exist/chronos_agents_404.yaml")
	if err == nil {
		t.Fatal("expected not found error")
	}
}

func TestLoadFile_InvalidYAML_Deep(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(p, []byte("agents: [\n  broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile(p)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestFileConfig_FindTeam_NotFound_Deep(t *testing.T) {
	fc := &FileConfig{Teams: []TeamConfig{{ID: "only", Name: "O"}}}
	_, err := fc.FindTeam("missing")
	if err == nil {
		t.Fatal("expected find team error")
	}
}

func TestBuildAgent_UnknownStorageBackend_Deep(t *testing.T) {
	cfg := &AgentConfig{
		ID: "a", Name: "A",
		Model: ModelConfig{Provider: "openai", APIKey: "k", Model: "gpt-4o"},
		Storage: StorageConfig{
			Backend: "cosmodb",
			DSN:     "x",
		},
	}
	_, err := BuildAgent(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected storage backend error")
	}
}

func TestBuildAgent_PostgresDSNMissing_Deep(t *testing.T) {
	cfg := &AgentConfig{
		ID: "a", Name: "A",
		Model: ModelConfig{Provider: "openai", APIKey: "k", Model: "gpt-4o"},
		Storage: StorageConfig{
			Backend: "postgres",
			DSN:     "",
		},
	}
	_, err := BuildAgent(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected postgres dsn error")
	}
}

func TestBuildAll_SubAgentMissing_Deep(t *testing.T) {
	fc := &FileConfig{
		Agents: []AgentConfig{
			{ID: "parent", Name: "P", Model: ModelConfig{Provider: "openai", APIKey: "k", Model: "gpt-4o"}, SubAgents: []string{"ghost"}},
		},
	}
	_, err := BuildAll(context.Background(), fc)
	if err == nil {
		t.Fatal("expected sub-agent missing error")
	}
}
