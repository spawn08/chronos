package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFile(t *testing.T) {
	yaml := `
agents:
  - id: test-agent
    name: Test Agent
    description: A test agent
    model:
      provider: ollama
      model: llama3.3
    system_prompt: You are a test agent.
    instructions:
      - Be concise
      - Use Go conventions
    capabilities:
      - code
      - test
`
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write test config: %v", err)
	}

	fc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(fc.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(fc.Agents))
	}

	cfg := fc.Agents[0]
	if cfg.ID != "test-agent" {
		t.Errorf("expected ID 'test-agent', got %q", cfg.ID)
	}
	if cfg.Name != "Test Agent" {
		t.Errorf("expected name 'Test Agent', got %q", cfg.Name)
	}
	if cfg.Model.Provider != "ollama" {
		t.Errorf("expected provider 'ollama', got %q", cfg.Model.Provider)
	}
	if cfg.Model.Model != "llama3.3" {
		t.Errorf("expected model 'llama3.3', got %q", cfg.Model.Model)
	}
	if cfg.System != "You are a test agent." {
		t.Errorf("unexpected system prompt: %q", cfg.System)
	}
	if len(cfg.Instructions) != 2 {
		t.Errorf("expected 2 instructions, got %d", len(cfg.Instructions))
	}
	if len(cfg.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities, got %d", len(cfg.Capabilities))
	}
}

func TestLoadFileWithDefaults(t *testing.T) {
	yaml := `
defaults:
  model:
    provider: openai
    api_key: default-key
    model: gpt-4o
  storage:
    backend: sqlite

agents:
  - id: agent-a
    name: Agent A
  - id: agent-b
    name: Agent B
    model:
      provider: anthropic
      model: claude-sonnet-4-6
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	fc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	a := fc.Agents[0]
	if a.Model.Provider != "openai" {
		t.Errorf("agent-a should inherit provider 'openai', got %q", a.Model.Provider)
	}
	if a.Model.Model != "gpt-4o" {
		t.Errorf("agent-a should inherit model 'gpt-4o', got %q", a.Model.Model)
	}
	if a.Model.APIKey != "default-key" {
		t.Errorf("agent-a should inherit api_key, got %q", a.Model.APIKey)
	}

	b := fc.Agents[1]
	if b.Model.Provider != "anthropic" {
		t.Errorf("agent-b should override to 'anthropic', got %q", b.Model.Provider)
	}
	if b.Model.Model != "claude-sonnet-4-6" {
		t.Errorf("agent-b should override model, got %q", b.Model.Model)
	}
	if b.Model.APIKey != "default-key" {
		t.Errorf("agent-b should inherit api_key, got %q", b.Model.APIKey)
	}
}

func TestFindAgent(t *testing.T) {
	fc := &FileConfig{
		Agents: []AgentConfig{
			{ID: "dev", Name: "Dev Agent"},
			{ID: "researcher", Name: "Research Agent"},
		},
	}

	tests := []struct {
		query   string
		wantID  string
		wantErr bool
	}{
		{"dev", "dev", false},
		{"Dev Agent", "dev", false},
		{"researcher", "researcher", false},
		{"RESEARCH AGENT", "researcher", false},
		{"nonexistent", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			cfg, err := fc.FindAgent(tt.query)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.ID != tt.wantID {
				t.Errorf("expected ID %q, got %q", tt.wantID, cfg.ID)
			}
		})
	}
}

func TestEnvExpansion(t *testing.T) {
	os.Setenv("TEST_CHRONOS_KEY", "my-secret-key")
	defer os.Unsetenv("TEST_CHRONOS_KEY")

	yaml := `
agents:
  - id: env-test
    name: Env Test
    model:
      provider: openai
      api_key: ${TEST_CHRONOS_KEY}
      model: gpt-4o
`
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	fc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if fc.Agents[0].Model.APIKey != "my-secret-key" {
		t.Errorf("expected expanded key 'my-secret-key', got %q", fc.Agents[0].Model.APIKey)
	}
}

func TestBuildAgent(t *testing.T) {
	cfg := &AgentConfig{
		ID:   "build-test",
		Name: "Build Test Agent",
		Model: ModelConfig{
			Provider: "ollama",
			Model:    "llama3.3",
		},
		System:       "You are a test agent.",
		Instructions: []string{"Be concise"},
		Capabilities: []string{"test"},
		Storage: StorageConfig{
			Backend: "none",
		},
	}

	a, err := BuildAgent(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildAgent: %v", err)
	}
	if a.ID != "build-test" {
		t.Errorf("expected ID 'build-test', got %q", a.ID)
	}
	if a.Name != "Build Test Agent" {
		t.Errorf("expected name 'Build Test Agent', got %q", a.Name)
	}
	if a.SystemPrompt != "You are a test agent." {
		t.Errorf("unexpected system prompt: %q", a.SystemPrompt)
	}
	if len(a.Instructions) != 1 || a.Instructions[0] != "Be concise" {
		t.Errorf("unexpected instructions: %v", a.Instructions)
	}
	if a.Model == nil {
		t.Fatal("expected model to be set")
	}
	if a.Model.Name() != "ollama" {
		t.Errorf("expected model name 'ollama', got %q", a.Model.Name())
	}
}

func TestBuildAgentWithSubAgents(t *testing.T) {
	fc := &FileConfig{
		Agents: []AgentConfig{
			{
				ID:   "worker",
				Name: "Worker",
				Model: ModelConfig{
					Provider: "ollama",
					Model:    "llama3.3",
				},
				Storage: StorageConfig{Backend: "none"},
			},
			{
				ID:   "boss",
				Name: "Boss",
				Model: ModelConfig{
					Provider: "ollama",
					Model:    "llama3.3",
				},
				SubAgents: []string{"worker"},
				Storage:   StorageConfig{Backend: "none"},
			},
		},
	}

	agents, err := BuildAll(context.Background(), fc)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	boss := agents["boss"]
	if len(boss.SubAgents) != 1 {
		t.Fatalf("expected 1 sub-agent, got %d", len(boss.SubAgents))
	}
	if boss.SubAgents[0].ID != "worker" {
		t.Errorf("expected sub-agent 'worker', got %q", boss.SubAgents[0].ID)
	}
}
