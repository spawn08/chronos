package agent

import (
	"context"
	"testing"
)

func TestFindTeam(t *testing.T) {
	fc := &FileConfig{
		Teams: []TeamConfig{
			{ID: "alpha", Name: "Alpha Team"},
			{ID: "beta", Name: "Beta Team"},
		},
	}

	tests := []struct {
		query   string
		wantID  string
		wantErr bool
	}{
		{"alpha", "alpha", false},
		{"ALPHA", "alpha", false},
		{"beta", "beta", false},
		{"nonexistent", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			tc, err := fc.FindTeam(tt.query)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("FindTeam: %v", err)
			}
			if tc.ID != tt.wantID {
				t.Errorf("expected %q, got %q", tt.wantID, tc.ID)
			}
		})
	}
}

func TestBuildProvider_AllProviders(t *testing.T) {
	providers := []struct {
		name     string
		cfg      ModelConfig
		wantName string
	}{
		{"openai", ModelConfig{Provider: "openai", APIKey: "key", Model: "gpt-4o"}, "openai"},
		{"openai default model", ModelConfig{Provider: "openai", APIKey: "key"}, "openai"},
		{"anthropic", ModelConfig{Provider: "anthropic", APIKey: "key"}, "anthropic"},
		{"gemini", ModelConfig{Provider: "gemini", APIKey: "key"}, "gemini"},
		{"google", ModelConfig{Provider: "google", APIKey: "key"}, "gemini"},
		{"mistral", ModelConfig{Provider: "mistral", APIKey: "key"}, "mistral"},
		{"ollama", ModelConfig{Provider: "ollama"}, "ollama"},
		{"ollama with host", ModelConfig{Provider: "ollama", BaseURL: "http://localhost:11434", Model: "llama3"}, "ollama"},
		{"azure", ModelConfig{Provider: "azure", APIKey: "key", Deployment: "gpt4", APIVersion: "2024-02-01"}, "azure-openai"},
		{"groq", ModelConfig{Provider: "groq", APIKey: "key"}, "groq"},
		{"together", ModelConfig{Provider: "together", APIKey: "key"}, "together"},
		{"deepseek", ModelConfig{Provider: "deepseek", APIKey: "key"}, "deepseek"},
		{"openrouter", ModelConfig{Provider: "openrouter", APIKey: "key"}, "openrouter"},
		{"fireworks", ModelConfig{Provider: "fireworks", APIKey: "key"}, "fireworks"},
		{"perplexity", ModelConfig{Provider: "perplexity", APIKey: "key"}, "perplexity"},
		{"anyscale", ModelConfig{Provider: "anyscale", APIKey: "key"}, "anyscale"},
		{"compatible", ModelConfig{Provider: "compatible", BaseURL: "http://custom/v1", APIKey: "key"}, "compatible"},
		{"custom", ModelConfig{Provider: "custom", BaseURL: "http://custom/v1", APIKey: "key"}, "custom"},
	}
	for _, tt := range providers {
		t.Run(tt.name, func(t *testing.T) {
			p, err := buildProvider(tt.cfg)
			if err != nil {
				t.Fatalf("buildProvider(%q): %v", tt.cfg.Provider, err)
			}
			if p == nil {
				t.Fatal("expected non-nil provider")
			}
			if p.Name() != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, p.Name())
			}
		})
	}
}

func TestBuildProvider_UnknownProvider(t *testing.T) {
	_, err := buildProvider(ModelConfig{Provider: "unknown-xyz"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestBuildStorage_Backends(t *testing.T) {
	tests := []struct {
		name    string
		cfg     StorageConfig
		wantNil bool
		wantErr bool
	}{
		{"none", StorageConfig{Backend: "none"}, true, false},
		{"memory", StorageConfig{Backend: "memory"}, true, false},
		{"empty defaults to sqlite", StorageConfig{}, false, false},
		{"sqlite explicit", StorageConfig{Backend: "sqlite", DSN: ":memory:"}, false, false},
		{"postgres no DSN", StorageConfig{Backend: "postgres"}, false, true},
		{"unknown", StorageConfig{Backend: "unknowndb"}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := buildStorage(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("buildStorage: %v", err)
			}
			if tt.wantNil && s != nil {
				t.Error("expected nil storage")
			}
			if !tt.wantNil && s == nil {
				t.Error("expected non-nil storage")
			}
		})
	}
}

func TestBuildAgent_AllProviders(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *AgentConfig
		wantErr bool
	}{
		{
			name: "anthropic",
			cfg: &AgentConfig{
				ID: "a1", Name: "A1",
				Model:   ModelConfig{Provider: "anthropic", APIKey: "k"},
				Storage: StorageConfig{Backend: "none"},
			},
		},
		{
			name: "unknown provider",
			cfg: &AgentConfig{
				ID: "a2", Name: "A2",
				Model:   ModelConfig{Provider: "fakeone"},
				Storage: StorageConfig{Backend: "none"},
			},
			wantErr: true,
		},
		{
			name: "with context config",
			cfg: &AgentConfig{
				ID: "a3", Name: "A3",
				Model:   ModelConfig{Provider: "ollama"},
				Storage: StorageConfig{Backend: "none"},
				Context: ContextYAML{MaxTokens: 4096, SummarizeThreshold: 0.8, PreserveRecentTurns: 2},
			},
		},
		{
			name: "with output schema",
			cfg: &AgentConfig{
				ID:           "a4",
				Name:         "A4",
				Model:        ModelConfig{Provider: "ollama"},
				Storage:      StorageConfig{Backend: "none"},
				OutputSchema: map[string]any{"type": "object"},
			},
		},
		{
			name: "with history runs",
			cfg: &AgentConfig{
				ID: "a5", Name: "A5",
				Model:          ModelConfig{Provider: "ollama"},
				Storage:        StorageConfig{Backend: "none"},
				NumHistoryRuns: 5,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildAgent(context.Background(), tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildAgent: %v", err)
			}
		})
	}
}

func TestReadConfigFile_EmptyPath(t *testing.T) {
	// Empty path should look in default locations and fail (no agents.yaml in test env)
	_, _, err := readConfigFile("")
	if err == nil {
		t.Skip("found an agents.yaml config file in default location - skipping")
	}
	// Should return an error mentioning the search locations
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestReadConfigFile_NonExistentPath(t *testing.T) {
	_, _, err := readConfigFile("/nonexistent/path/agents.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestBuildStorage_NoneBackend(t *testing.T) {
	store, err := buildStorage(StorageConfig{Backend: "none"})
	if err != nil {
		t.Fatalf("buildStorage none: %v", err)
	}
	if store != nil {
		t.Error("expected nil store for 'none' backend")
	}
}

func TestBuildStorage_MemoryBackend(t *testing.T) {
	store, err := buildStorage(StorageConfig{Backend: "memory"})
	if err != nil {
		t.Fatalf("buildStorage memory: %v", err)
	}
	// memory and none both return nil store
	_ = store
}
