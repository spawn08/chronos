package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spawn08/chronos/engine/tool"
)

func TestBuildToolFromConfigPermissionOverrides(t *testing.T) {
	confirmation := false
	def, err := buildToolFromConfig(ToolConfig{
		Name:                 "file_write",
		Permission:           tool.PermAllow,
		RequiresConfirmation: &confirmation,
	}, newToolHandlerRegistry())
	if err != nil {
		t.Fatalf("buildToolFromConfig: %v", err)
	}
	if def.Permission != tool.PermAllow {
		t.Fatalf("permission = %q, want allow", def.Permission)
	}
	if def.RequiresConfirmation {
		t.Fatal("requires_confirmation override was not applied")
	}

	if _, err := buildToolFromConfig(ToolConfig{Name: "file_read", Permission: "maybe"}, newToolHandlerRegistry()); err == nil {
		t.Fatal("expected invalid permission error")
	}
}

func TestLoadFileRuntimeConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.yaml")
	data := []byte(`defaults:
  stream: true
  permission_mode: auto_approve
  debug: true
  tracing: true
  reasoning:
    strategy: reflection
    native: true
    effort: high
    budget_tokens: 2048
    summary: true
agents:
  - id: dev
    name: Dev
    model:
      provider: openai
      model: gpt-test
    tools:
      - name: file_write
        permission: allow
        requires_confirmation: false
  - id: quiet
    name: Quiet
    stream: false
    model:
      provider: openai
      model: gpt-test
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got := cfg.Agents[0]
	if !got.StreamConfigured || !got.Stream || !got.Debug || !got.Tracing {
		t.Fatalf("runtime defaults not applied: %#v", got)
	}
	if got.PermissionMode != tool.PermissionModeAutoApprove {
		t.Fatalf("permission mode = %q", got.PermissionMode)
	}
	if got.Reasoning.Strategy != "reflection" || !got.Reasoning.Native || got.Reasoning.BudgetTokens != 2048 {
		t.Fatalf("reasoning defaults not applied: %#v", got.Reasoning)
	}
	quiet := cfg.Agents[1]
	if !quiet.StreamConfigured || quiet.Stream {
		t.Fatalf("explicit stream:false did not override defaults: %#v", quiet)
	}
}

func TestLoadFileRejectsUnknownAndInvalidRuntimeFields(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "unknown field",
			yaml: "agents:\n  - id: dev\n    name: Dev\n    model:\n      provider: openai\n    permision_mode: bypass\n",
		},
		{
			name: "invalid permission mode",
			yaml: "agents:\n  - id: dev\n    name: Dev\n    model:\n      provider: openai\n    permission_mode: anything\n",
		},
		{
			name: "invalid reasoning effort",
			yaml: "agents:\n  - id: dev\n    name: Dev\n    model:\n      provider: openai\n    reasoning:\n      effort: extreme\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agents.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if _, err := LoadFile(path); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestBuildAgentRuntimeConfiguration(t *testing.T) {
	cfg := &AgentConfig{
		ID:               "dev",
		Name:             "Dev",
		Model:            ModelConfig{Provider: "openai", Model: "gpt-test"},
		Storage:          StorageConfig{Backend: "sqlite", DSN: ":memory:"},
		Stream:           false,
		StreamConfigured: true,
		Debug:            true,
		Tracing:          true,
		PermissionMode:   tool.PermissionModeAutoApprove,
		Reasoning: ReasoningYAML{
			Strategy:     "cot",
			Native:       true,
			Effort:       "medium",
			BudgetTokens: 1024,
			Summary:      true,
		},
		Tools: []ToolConfig{{Name: "file_write", Permission: tool.PermAllow}},
	}
	a, err := BuildAgent(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildAgent: %v", err)
	}
	defer func() { _ = a.Storage.Close() }()

	if !a.StreamConfigured || a.Stream || !a.Debug || a.Tracer == nil {
		t.Fatalf("runtime config not wired: stream=%t configured=%t debug=%t tracer=%v", a.Stream, a.StreamConfigured, a.Debug, a.Tracer)
	}
	if a.Tools.PermissionMode() != tool.PermissionModeAutoApprove {
		t.Fatalf("permission mode = %q", a.Tools.PermissionMode())
	}
	if a.Reasoning != ReasoningCoT || !a.ReasoningConfig.Enabled || a.ReasoningConfig.Effort != "medium" {
		t.Fatalf("reasoning not wired: strategy=%v config=%#v", a.Reasoning, a.ReasoningConfig)
	}
	def, ok := a.Tools.Get("file_write")
	if !ok || def.Permission != tool.PermAllow {
		t.Fatalf("tool permission not wired: %#v", def)
	}
}
