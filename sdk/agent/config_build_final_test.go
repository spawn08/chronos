package agent

import (
	"context"
	"strings"
	"testing"
)

func TestBuildAgent_Providers_NoNetwork(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		provider string
		model    string
	}{
		{"groq", "llama"},
		{"together", "m"},
		{"deepseek", "d"},
		{"openrouter", "or"},
		{"fireworks", "f"},
		{"perplexity", "p"},
		{"anyscale", "a"},
		{"compatible", "c"},
		{"custom", "x"},
		{"Google", "g"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			cfg := &AgentConfig{
				ID: "id1", Name: "n",
				Model: ModelConfig{
					Provider: tc.provider,
					Model:    tc.model,
					APIKey:   "test-key",
					BaseURL:  "http://localhost:9",
				},
				Storage: StorageConfig{Backend: "none"},
			}
			a, err := BuildAgent(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if a.Model == nil {
				t.Fatal("expected model")
			}
		})
	}
}

func TestBuildAgent_ContextYAMLPartial(t *testing.T) {
	ctx := context.Background()
	cfg := &AgentConfig{
		ID: "cx", Name: "cx",
		Model:   ModelConfig{Provider: "openai", APIKey: "k"},
		Storage: StorageConfig{Backend: "none"},
		Context: ContextYAML{SummarizeThreshold: 0.5},
	}
	a, err := BuildAgent(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if a.ContextCfg.SummarizeThreshold != 0.5 {
		t.Fatalf("threshold %v", a.ContextCfg.SummarizeThreshold)
	}
}

func TestBuildStorage_PostgresErrors(t *testing.T) {
	_, err := buildStorage(StorageConfig{Backend: "postgres"})
	if err == nil || !strings.Contains(err.Error(), "dsn") {
		t.Fatalf("expected dsn error, got %v", err)
	}
	_, err = buildStorage(StorageConfig{Backend: "postgres", DSN: "x"})
	if err == nil || !strings.Contains(err.Error(), "programmatically") {
		t.Fatalf("expected programmatically error, got %v", err)
	}
}

func TestBuildStorage_UnknownBackend(t *testing.T) {
	_, err := buildStorage(StorageConfig{Backend: "cassandra"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildAll_SubAgentMissing(t *testing.T) {
	ctx := context.Background()
	fc := &FileConfig{
		Agents: []AgentConfig{
			{ID: "a1", Name: "A1", Model: ModelConfig{Provider: "openai", APIKey: "k"}, Storage: StorageConfig{Backend: "none"}},
			{ID: "a2", Name: "A2", Model: ModelConfig{Provider: "openai", APIKey: "k"}, Storage: StorageConfig{Backend: "none"}, SubAgents: []string{"ghost"}},
		},
	}
	_, err := BuildAll(ctx, fc)
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected missing sub-agent error, got %v", err)
	}
}
