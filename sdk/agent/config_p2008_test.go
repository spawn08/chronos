package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/storage"
)

// baseModelConfig returns a valid model config that constructs without
// contacting a network (no request is made during Build).
func baseModelConfig() ModelConfig {
	return ModelConfig{Provider: "openai", Model: "gpt-4o", APIKey: "test-key"}
}

func TestRegisterToolHandler_InvokedByBuiltAgent(t *testing.T) {
	const toolName = "p2008_echo"
	t.Cleanup(func() { UnregisterToolHandler(toolName) })

	var factoryCalls int
	RegisterToolHandler(toolName, func(tc ToolConfig) (tool.Handler, error) {
		factoryCalls++
		// The factory sees the declared config.
		if tc.Name != toolName {
			t.Errorf("factory got name %q, want %q", tc.Name, toolName)
		}
		return func(_ context.Context, args map[string]any) (any, error) {
			return map[string]any{"echoed": args["msg"]}, nil
		}, nil
	})

	cfg := &AgentConfig{
		ID:    "a1",
		Name:  "A1",
		Model: baseModelConfig(),
		Tools: []ToolConfig{{
			Name:        toolName,
			Description: "echoes its input",
			Parameters:  map[string]any{"type": "object"},
		}},
	}

	ag, err := BuildAgent(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildAgent: %v", err)
	}
	if factoryCalls != 1 {
		t.Fatalf("factory called %d times, want 1", factoryCalls)
	}

	def, ok := ag.Tools.Get(toolName)
	if !ok {
		t.Fatalf("tool %q not registered on agent", toolName)
	}
	if def.Description != "echoes its input" {
		t.Errorf("description = %q", def.Description)
	}

	out, err := ag.Tools.Execute(context.Background(), toolName, map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["echoed"] != "hi" {
		t.Fatalf("unexpected handler output: %#v", out)
	}
}

func TestBuildAgent_UnregisteredCustomTool_ErrorsOnInvoke(t *testing.T) {
	const toolName = "p2008_unregistered"
	// Ensure nothing is registered for this name.
	UnregisterToolHandler(toolName)

	cfg := &AgentConfig{
		ID:    "a2",
		Name:  "A2",
		Model: baseModelConfig(),
		Tools: []ToolConfig{{
			Name:        toolName,
			Description: "a custom tool with no handler wired",
		}},
	}

	ag, err := BuildAgent(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildAgent should succeed with a placeholder: %v", err)
	}

	// The tool exists (so the model knows about it) ...
	if _, ok := ag.Tools.Get(toolName); !ok {
		t.Fatalf("placeholder tool %q not registered", toolName)
	}

	// ... but invoking it errors rather than silently succeeding.
	out, err := ag.Tools.Execute(context.Background(), toolName, map[string]any{})
	if err == nil {
		t.Fatalf("expected error invoking unregistered custom tool, got output %#v", out)
	}
	if !strings.Contains(err.Error(), "no registered handler") {
		t.Fatalf("error = %q, want mention of missing handler", err.Error())
	}
	if !strings.Contains(err.Error(), "RegisterToolHandler") {
		t.Fatalf("error should point to RegisterToolHandler: %q", err.Error())
	}
}

func TestBuildAgent_FactoryError_Propagates(t *testing.T) {
	const toolName = "p2008_bad"
	t.Cleanup(func() { UnregisterToolHandler(toolName) })

	sentinel := errors.New("boom")
	RegisterToolHandler(toolName, func(ToolConfig) (tool.Handler, error) {
		return nil, sentinel
	})

	cfg := &AgentConfig{
		ID:    "a3",
		Name:  "A3",
		Model: baseModelConfig(),
		Tools: []ToolConfig{{Name: toolName, Description: "fails to build"}},
	}

	_, err := BuildAgent(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected BuildAgent to fail when factory errors")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error should wrap factory error: %v", err)
	}
}

func TestBuildToolFromConfig_Cases(t *testing.T) {
	const withHandler = "p2008_withhandler"
	const nilHandler = "p2008_nilhandler"
	t.Cleanup(func() {
		UnregisterToolHandler(withHandler)
		UnregisterToolHandler(nilHandler)
	})
	RegisterToolHandler(withHandler, func(ToolConfig) (tool.Handler, error) {
		return func(context.Context, map[string]any) (any, error) { return "ok", nil }, nil
	})
	RegisterToolHandler(nilHandler, func(ToolConfig) (tool.Handler, error) { return nil, nil })

	tests := []struct {
		name       string
		tc         ToolConfig
		wantNil    bool
		wantErr    bool
		wantInvoke string // "ok", "err", or "" to skip
	}{
		{name: "builtin shell", tc: ToolConfig{Name: "shell"}},
		{name: "builtin file_read", tc: ToolConfig{Name: "file_read"}},
		{name: "custom no description skipped", tc: ToolConfig{Name: "unknown_no_desc"}, wantNil: true},
		{name: "custom placeholder errors on invoke", tc: ToolConfig{Name: "unknown_desc", Description: "d"}, wantInvoke: "err"},
		{name: "registered handler", tc: ToolConfig{Name: withHandler, Description: "d"}, wantInvoke: "ok"},
		{name: "registered nil handler is error", tc: ToolConfig{Name: nilHandler, Description: "d"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def, err := buildToolFromConfig(tt.tc)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got def=%#v", def)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if def != nil {
					t.Fatalf("expected nil def, got %#v", def)
				}
				return
			}
			if def == nil {
				t.Fatal("expected non-nil def")
			}
			switch tt.wantInvoke {
			case "ok":
				out, err := def.Handler(context.Background(), nil)
				if err != nil || out != "ok" {
					t.Fatalf("invoke = %v, %v", out, err)
				}
			case "err":
				if _, err := def.Handler(context.Background(), nil); err == nil {
					t.Fatal("expected placeholder handler to error")
				}
			}
		})
	}
}

func TestRegisterToolHandler_Guards(t *testing.T) {
	// Empty name and nil factory are no-ops (never registered).
	RegisterToolHandler("", func(ToolConfig) (tool.Handler, error) { return nil, nil })
	if _, ok := lookupToolHandler(""); ok {
		t.Fatal("empty name should not register")
	}
	const n = "p2008_guard"
	RegisterToolHandler(n, nil)
	if _, ok := lookupToolHandler(n); ok {
		t.Fatal("nil factory should not register")
	}
}

func TestBuildStorage_SQLiteRealStore(t *testing.T) {
	store, err := buildStorage(StorageConfig{
		Backend:      "sqlite",
		DSN:          ":memory:",
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("buildStorage sqlite: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil sqlite store")
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Prove it is a real store: write and read back a session.
	s := &storage.Session{
		ID:        "sess-1",
		AgentID:   "a",
		Status:    "running",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := store.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.AgentID != "a" {
		t.Fatalf("round-trip mismatch: %#v", got)
	}
}

func TestBuildStorage_PostgresConstructs(t *testing.T) {
	// Without a live server the pgx driver opens lazily, so construction with a
	// valid DSN and pool tuning must succeed and yield a real *Store.
	store, err := buildStorage(StorageConfig{
		Backend:            "postgres",
		DSN:                "postgres://user:pass@localhost:5432/chronos?sslmode=disable",
		MaxOpenConns:       10,
		MaxIdleConns:       2,
		ConnMaxLifetimeSec: 60,
	})
	if err != nil {
		t.Fatalf("buildStorage postgres: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil postgres store")
	}
	defer func() { _ = store.Close() }()

	// Missing DSN is a clear error.
	if _, err := buildStorage(StorageConfig{Backend: "postgresql"}); err == nil {
		t.Fatal("expected error for postgres without dsn")
	}

	// Optionally exercise a live server when one is provided.
	dsn := os.Getenv("CHRONOS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Log("CHRONOS_TEST_POSTGRES_DSN not set; skipping live postgres check")
		return
	}
	live, err := buildStorage(StorageConfig{Backend: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("live buildStorage: %v", err)
	}
	defer func() { _ = live.Close() }()
	if err := live.Migrate(context.Background()); err != nil {
		t.Fatalf("live migrate: %v", err)
	}
}

// Ensure the placeholder error message is stable enough to guide users.
func TestPlaceholderErrorMessage(t *testing.T) {
	def, err := buildToolFromConfig(ToolConfig{Name: "some_custom_tool", Description: "d"})
	if err != nil || def == nil {
		t.Fatalf("build: def=%v err=%v", def, err)
	}
	_, err = def.Handler(context.Background(), nil)
	want := fmt.Sprintf("tool %q has no registered handler", "some_custom_tool")
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want contains %q", err, want)
	}
}
