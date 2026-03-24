package agent

import (
	"strings"
	"testing"
)

func TestExpandEnv_Empty(t *testing.T) {
	if expandEnv("") != "" {
		t.Error("empty string should stay empty")
	}
}

func TestExpandEnv_WithEnv(t *testing.T) {
	t.Setenv("CHRONOS_TEST_EXPAND", "xyzzy")
	got := expandEnv("${CHRONOS_TEST_EXPAND}")
	if got != "xyzzy" {
		t.Errorf("got %q, want xyzzy", got)
	}
}

func TestApplyDefaults_PartialModel(t *testing.T) {
	cfg := &AgentConfig{
		Model: ModelConfig{Provider: ""},
	}
	defaults := &AgentConfig{
		Model: ModelConfig{Provider: "ollama", Model: "m", APIKey: "k"},
	}
	applyDefaults(cfg, defaults)
	if cfg.Model.Provider != "ollama" {
		t.Errorf("Provider = %q", cfg.Model.Provider)
	}
	if cfg.Model.Model != "m" {
		t.Errorf("Model = %q", cfg.Model.Model)
	}
}

func TestFileConfig_FindAgent_NotFoundMessage(t *testing.T) {
	fc := &FileConfig{
		Agents: []AgentConfig{{ID: "only", Name: "One"}},
	}
	_, err := fc.FindAgent("missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error: %v", err)
	}
}

func TestBuildStorage_PostgresqlDSNErrorMessage(t *testing.T) {
	_, err := buildStorage(StorageConfig{Backend: "postgresql", DSN: "postgres://localhost/x"})
	if err == nil {
		t.Fatal("expected error for postgres programmatic setup")
	}
}

func TestBuildProvider_CompatibleName(t *testing.T) {
	p, err := buildProvider(ModelConfig{Provider: "compatible", BaseURL: "http://127.0.0.1:1/v1", APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "compatible" {
		t.Errorf("name = %q", p.Name())
	}
}

func TestExpandEnvInConfig_NoPanic(t *testing.T) {
	cfg := &AgentConfig{
		ID:          "id",
		Name:        "n",
		System:      "s",
		Instructions: []string{"a"},
		Model:       ModelConfig{APIKey: "k"},
		Storage:     StorageConfig{DSN: "d"},
	}
	expandEnvInConfig(cfg)
	_ = cfg.ID
}
