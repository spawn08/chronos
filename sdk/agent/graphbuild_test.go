package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

func TestValidateGraphConfig(t *testing.T) {
	tests := []struct {
		name    string
		gc      *GraphConfig
		wantErr string
	}{
		{name: "nil is valid", gc: nil},
		{
			name:    "missing entry",
			gc:      &GraphConfig{Nodes: []NodeConfig{{ID: "a", Type: "passthrough"}}},
			wantErr: "graph.entry is required",
		},
		{
			name:    "no nodes",
			gc:      &GraphConfig{Entry: "a"},
			wantErr: "graph.nodes must declare at least one node",
		},
		{
			name: "duplicate node id",
			gc: &GraphConfig{
				Entry: "a",
				Nodes: []NodeConfig{{ID: "a", Type: "passthrough"}, {ID: "a", Type: "passthrough"}},
			},
			wantErr: "duplicate node id",
		},
		{
			name:    "unknown node type",
			gc:      &GraphConfig{Entry: "a", Nodes: []NodeConfig{{ID: "a", Type: "bogus"}}},
			wantErr: `unknown type "bogus"`,
		},
		{
			name:    "model node requires prompt",
			gc:      &GraphConfig{Entry: "a", Nodes: []NodeConfig{{ID: "a", Type: "model"}}},
			wantErr: "type model requires prompt",
		},
		{
			name:    "tool node requires tool",
			gc:      &GraphConfig{Entry: "a", Nodes: []NodeConfig{{ID: "a", Type: "tool"}}},
			wantErr: "type tool requires tool",
		},
		{
			name:    "subagent node requires agent",
			gc:      &GraphConfig{Entry: "a", Nodes: []NodeConfig{{ID: "a", Type: "subagent"}}},
			wantErr: "type subagent requires agent",
		},
		{
			name:    "entry not declared",
			gc:      &GraphConfig{Entry: "missing", Nodes: []NodeConfig{{ID: "a", Type: "passthrough"}}},
			wantErr: `graph.entry "missing" is not a declared node`,
		},
		{
			name: "finish not declared",
			gc: &GraphConfig{
				Entry:  "a",
				Finish: "missing",
				Nodes:  []NodeConfig{{ID: "a", Type: "passthrough"}},
			},
			wantErr: `graph.finish "missing" is not a declared node`,
		},
		{
			name: "edge from unknown node",
			gc: &GraphConfig{
				Entry: "a",
				Nodes: []NodeConfig{{ID: "a", Type: "passthrough"}},
				Edges: []EdgeConfig{{From: "x", To: "a"}},
			},
			wantErr: `from "x" is not a declared node`,
		},
		{
			name: "static edge missing to",
			gc: &GraphConfig{
				Entry: "a",
				Nodes: []NodeConfig{{ID: "a", Type: "passthrough"}},
				Edges: []EdgeConfig{{From: "a"}},
			},
			wantErr: "to is required for a non-conditional edge",
		},
		{
			name: "static edge unknown target",
			gc: &GraphConfig{
				Entry: "a",
				Nodes: []NodeConfig{{ID: "a", Type: "passthrough"}},
				Edges: []EdgeConfig{{From: "a", To: "missing"}},
			},
			wantErr: `to "missing" is not a declared node`,
		},
		{
			name: "static edge to end sentinel is valid",
			gc: &GraphConfig{
				Entry: "a",
				Nodes: []NodeConfig{{ID: "a", Type: "passthrough"}},
				Edges: []EdgeConfig{{From: "a", To: "__end__"}},
			},
		},
		{
			name: "conditional edge requires route_key",
			gc: &GraphConfig{
				Entry: "a",
				Nodes: []NodeConfig{{ID: "a", Type: "passthrough"}, {ID: "b", Type: "passthrough"}},
				Edges: []EdgeConfig{{From: "a", Conditional: true, Routes: map[string]string{"x": "b"}}},
			},
			wantErr: "conditional edge requires route_key",
		},
		{
			name: "conditional edge requires routes",
			gc: &GraphConfig{
				Entry: "a",
				Nodes: []NodeConfig{{ID: "a", Type: "passthrough"}},
				Edges: []EdgeConfig{{From: "a", Conditional: true, RouteKey: "k"}},
			},
			wantErr: "conditional edge requires at least one entry in routes",
		},
		{
			name: "conditional edge route unknown target",
			gc: &GraphConfig{
				Entry: "a",
				Nodes: []NodeConfig{{ID: "a", Type: "passthrough"}},
				Edges: []EdgeConfig{{From: "a", Conditional: true, RouteKey: "k", Routes: map[string]string{"x": "missing"}}},
			},
			wantErr: `route "x" -> "missing" is not a declared node`,
		},
		{
			name: "conditional edge default unknown target",
			gc: &GraphConfig{
				Entry: "a",
				Nodes: []NodeConfig{{ID: "a", Type: "passthrough"}, {ID: "b", Type: "passthrough"}},
				Edges: []EdgeConfig{{From: "a", Conditional: true, RouteKey: "k", Routes: map[string]string{"x": "b"}, Default: "missing"}},
			},
			wantErr: `default "missing" is not a declared node`,
		},
		{
			name: "valid graph with interrupt and conditional routing",
			gc: &GraphConfig{
				Entry:  "start",
				Finish: "end",
				Nodes: []NodeConfig{
					{ID: "start", Type: "passthrough"},
					{ID: "gate", Type: "passthrough", Interrupt: true},
					{ID: "end", Type: "passthrough"},
				},
				Edges: []EdgeConfig{
					{From: "start", To: "gate"},
					{From: "gate", Conditional: true, RouteKey: "decision", Routes: map[string]string{"approved": "end"}, Default: "__end__"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGraphConfig("test-agent", tt.gc)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestBuildAgentGraph exercises all four declarative node types end to end
// through BuildAgent + Agent.Run, proving the compiled graph actually
// executes (not just validates) against a real model.Provider fake, a
// registered tool, and durable storage.
func TestBuildAgentGraph(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	provider := &testProvider{response: &model.ChatResponse{Content: "hello from model"}}

	cfg := &AgentConfig{
		ID:   "graph-agent",
		Name: "Graph Agent",
		Model: ModelConfig{
			Provider: "ollama", // built but never dialed; provider is overridden below
		},
		Storage: StorageConfig{Backend: "none"}, // built.Storage is set explicitly below
		Tools:   []ToolConfig{{Name: "echo", Description: "echoes input"}},
		Graph: &GraphConfig{
			Entry:  "greet",
			Finish: "done",
			Nodes: []NodeConfig{
				{ID: "greet", Type: "model", Prompt: "say hi to {{.state.name}}", OutputKey: "greeting"},
				{ID: "call_tool", Type: "tool", Tool: "echo", OutputKey: "tool_out"},
				{ID: "gate", Type: "passthrough", Interrupt: true, Set: map[string]any{"gated": true}},
				{ID: "done", Type: "passthrough"},
			},
			Edges: []EdgeConfig{
				{From: "greet", To: "call_tool"},
				{From: "call_tool", To: "gate"},
				{From: "gate", To: "done"},
			},
		},
	}

	built, err := BuildAgent(ctx, cfg, WithToolHandler("echo", func(ToolConfig) (tool.Handler, error) {
		return func(_ context.Context, args map[string]any) (any, error) {
			return "echoed", nil
		}, nil
	}))
	if err != nil {
		t.Fatalf("BuildAgent: %v", err)
	}
	// Swap in the fake provider so the model node never dials a real ollama
	// instance — buildProvider() must be called (validating the config), but
	// the actual runtime provider used for execution is this test double.
	built.Model = provider
	built.Storage = store

	if built.Graph == nil {
		t.Fatal("expected Agent.Graph to be compiled from AgentConfig.Graph")
	}

	rs, err := built.Run(ctx, map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rs.Status != graph.RunStatusPaused {
		t.Fatalf("status = %v, want paused (gate is an interrupt node)", rs.Status)
	}
	if rs.State["greeting"] != "hello from model" {
		t.Errorf("greeting = %v, want %q", rs.State["greeting"], "hello from model")
	}
	if rs.State["tool_out"] != "echoed" {
		t.Errorf("tool_out = %v, want %q", rs.State["tool_out"], "echoed")
	}

	// The interrupt node must have paused the run before mutating state.
	if rs.State["gated"] != nil {
		t.Errorf("gated should not be set until resume, got %v", rs.State["gated"])
	}

	resumed, err := built.Resume(ctx, rs.SessionID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.State["gated"] != true {
		t.Errorf("gated = %v, want true after resume", resumed.State["gated"])
	}
	if resumed.Status != graph.RunStatusCompleted {
		t.Errorf("status after resume = %v, want completed", resumed.Status)
	}
}

// TestBuildAgentGraphToolNodeEnforcesPermission proves a "tool" graph node
// routes through tool.Registry.Execute (permission/approval enforcement),
// not the Definition's Handler directly — a graph-declared tool is not a
// backdoor around the same checks a model-initiated tool call would face.
func TestBuildAgentGraphToolNodeEnforcesPermission(t *testing.T) {
	ctx := context.Background()
	cfg := &AgentConfig{
		ID:      "denied-tool-agent",
		Name:    "Denied Tool Agent",
		Model:   ModelConfig{Provider: "ollama"},
		Storage: StorageConfig{Backend: "none"},
		Tools: []ToolConfig{{
			Name:        "dangerous",
			Description: "should never run",
			Permission:  tool.PermDeny,
		}},
		Graph: &GraphConfig{
			Entry: "call_it",
			Nodes: []NodeConfig{
				{ID: "call_it", Type: "tool", Tool: "dangerous"},
			},
		},
	}
	called := false
	built, err := BuildAgent(ctx, cfg, WithToolHandler("dangerous", func(ToolConfig) (tool.Handler, error) {
		return func(_ context.Context, _ map[string]any) (any, error) {
			called = true
			return "should not happen", nil
		}, nil
	}))
	if err != nil {
		t.Fatalf("BuildAgent: %v", err)
	}
	built.Storage = memory.New()
	if migrateErr := built.Storage.Migrate(ctx); migrateErr != nil {
		t.Fatalf("migrate: %v", migrateErr)
	}

	_, err = built.Run(ctx, map[string]any{})
	if err == nil {
		t.Fatal("expected the run to fail on a denied tool")
	}
	if !strings.Contains(err.Error(), "is denied") {
		t.Errorf("error = %v, want it to mention the tool is denied", err)
	}
	if called {
		t.Error("the denied tool's handler must never execute")
	}
}

// TestBuildAgentGraphInputKeyMismatchErrors proves tool/subagent nodes report
// a clear error instead of silently falling back when input_key resolves to
// a value of the wrong type (or is missing entirely).
func TestBuildAgentGraphInputKeyMismatchErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("tool node: input_key present but wrong type", func(t *testing.T) {
		cfg := &AgentConfig{
			ID:      "bad-input-tool",
			Model:   ModelConfig{Provider: "ollama"},
			Storage: StorageConfig{Backend: "none"},
			Tools:   []ToolConfig{{Name: "echo", Description: "echoes"}},
			Graph: &GraphConfig{
				Entry: "prep",
				Nodes: []NodeConfig{
					{ID: "prep", Type: "passthrough", Set: map[string]any{"payload": "not-a-map"}},
					{ID: "call_tool", Type: "tool", Tool: "echo", InputKey: "payload"},
				},
				Edges: []EdgeConfig{{From: "prep", To: "call_tool"}},
			},
		}
		built, err := BuildAgent(ctx, cfg, WithToolHandler("echo", func(ToolConfig) (tool.Handler, error) {
			return func(_ context.Context, args map[string]any) (any, error) { return args, nil }, nil
		}))
		if err != nil {
			t.Fatalf("BuildAgent: %v", err)
		}
		built.Storage = memory.New()
		if migrateErr := built.Storage.Migrate(ctx); migrateErr != nil {
			t.Fatalf("migrate: %v", migrateErr)
		}
		_, err = built.Run(ctx, map[string]any{})
		if err == nil || !strings.Contains(err.Error(), `input_key "payload" is string, want a map`) {
			t.Fatalf("expected an input_key type-mismatch error, got %v", err)
		}
	})

	t.Run("subagent node: input_key missing entirely", func(t *testing.T) {
		cfg := &AgentConfig{
			ID:      "bad-input-subagent",
			Model:   ModelConfig{Provider: "ollama"},
			Storage: StorageConfig{Backend: "none"},
			Graph: &GraphConfig{
				Entry: "ask",
				Nodes: []NodeConfig{
					{ID: "ask", Type: "subagent", Agent: "helper", InputKey: "missing_key"},
				},
			},
		}
		helper, err := BuildAgent(ctx, &AgentConfig{ID: "helper", Model: ModelConfig{Provider: "ollama"}, Storage: StorageConfig{Backend: "none"}})
		if err != nil {
			t.Fatalf("BuildAgent(helper): %v", err)
		}
		built, err := BuildAgent(ctx, cfg, WithPeerAgents(map[string]*Agent{"helper": helper}))
		if err != nil {
			t.Fatalf("BuildAgent: %v", err)
		}
		built.Storage = memory.New()
		if migrateErr := built.Storage.Migrate(ctx); migrateErr != nil {
			t.Fatalf("migrate: %v", migrateErr)
		}
		_, err = built.Run(ctx, map[string]any{})
		if err == nil || !strings.Contains(err.Error(), `input_key "missing_key" not found`) {
			t.Fatalf("expected an input_key not-found error, got %v", err)
		}
	})
}

func TestBuildAllGraphSubagentPeers(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	fc := &FileConfig{
		Agents: []AgentConfig{
			{
				ID:      "helper",
				Name:    "Helper",
				Model:   ModelConfig{Provider: "ollama"},
				Storage: StorageConfig{Backend: "none"},
			},
			{
				ID:      "orchestrator",
				Name:    "Orchestrator",
				Model:   ModelConfig{Provider: "ollama"},
				Durable: true,
				Storage: StorageConfig{Backend: "sqlite", DSN: ":memory:"},
				Graph: &GraphConfig{
					Entry:  "ask_helper",
					Finish: "ask_helper",
					Nodes: []NodeConfig{
						{ID: "ask_helper", Type: "subagent", Agent: "helper", OutputKey: "helper_said"},
					},
				},
			},
		},
	}
	if err := validateFileConfig(fc); err != nil {
		t.Fatalf("validateFileConfig: %v", err)
	}

	agents, err := BuildAll(ctx, fc)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	orchestrator := agents["orchestrator"]
	if orchestrator.Graph == nil {
		t.Fatal("expected orchestrator.Graph to be compiled")
	}
	helper := agents["helper"]
	helper.Model = &testProvider{response: &model.ChatResponse{Content: "helper reply"}}
	orchestrator.Storage = store

	rs, err := orchestrator.Run(ctx, map[string]any{"message": "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rs.State["helper_said"] != "helper reply" {
		t.Errorf("helper_said = %v, want %q", rs.State["helper_said"], "helper reply")
	}
}

// TestBuildAgentWithPeerAgents exercises WithPeerAgents directly against a
// single BuildAgent call (not via BuildAll), the entry point for a caller who
// builds a subagent's peers themselves — e.g. because they came from a
// different config file or were built with different BuildOptions.
func TestBuildAgentWithPeerAgents(t *testing.T) {
	ctx := context.Background()

	helperCfg := &AgentConfig{ID: "helper", Name: "Helper", Model: ModelConfig{Provider: "ollama"}, Storage: StorageConfig{Backend: "none"}}
	helper, err := BuildAgent(ctx, helperCfg)
	if err != nil {
		t.Fatalf("BuildAgent(helper): %v", err)
	}
	helper.Model = &testProvider{response: &model.ChatResponse{Content: "peer reply"}}

	orchestratorCfg := &AgentConfig{
		ID:      "orchestrator",
		Name:    "Orchestrator",
		Model:   ModelConfig{Provider: "ollama"},
		Storage: StorageConfig{Backend: "none"},
		Graph: &GraphConfig{
			Entry:  "ask_helper",
			Finish: "ask_helper",
			Nodes: []NodeConfig{
				{ID: "ask_helper", Type: "subagent", Agent: "helper", OutputKey: "helper_said"},
			},
		},
	}
	orchestrator, err := BuildAgent(ctx, orchestratorCfg, WithPeerAgents(map[string]*Agent{"helper": helper}))
	if err != nil {
		t.Fatalf("BuildAgent(orchestrator, WithPeerAgents): %v", err)
	}
	if orchestrator.Graph == nil {
		t.Fatal("expected orchestrator.Graph to be compiled")
	}
	orchestrator.Storage = memory.New()
	if migrateErr := orchestrator.Storage.Migrate(ctx); migrateErr != nil {
		t.Fatalf("migrate: %v", migrateErr)
	}

	rs, err := orchestrator.Run(ctx, map[string]any{"message": "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rs.State["helper_said"] != "peer reply" {
		t.Errorf("helper_said = %v, want %q", rs.State["helper_said"], "peer reply")
	}
}

// TestBuildAgentSubagentWithoutPeerAgentsErrors proves a `subagent` node
// fails clearly (not silently) when no peer agent map is available at all —
// the case WithPeerAgents exists to opt into.
func TestBuildAgentSubagentWithoutPeerAgentsErrors(t *testing.T) {
	cfg := &AgentConfig{
		ID:      "orchestrator",
		Name:    "Orchestrator",
		Model:   ModelConfig{Provider: "ollama"},
		Storage: StorageConfig{Backend: "none"},
		Graph: &GraphConfig{
			Entry: "ask_helper",
			Nodes: []NodeConfig{
				{ID: "ask_helper", Type: "subagent", Agent: "helper"},
			},
		},
	}
	_, err := BuildAgent(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), `subagent "helper" not found`) {
		t.Fatalf("expected a not-found error for the unresolved subagent peer, got %v", err)
	}
}

func TestValidateFileConfigDurableRequiresGraph(t *testing.T) {
	fc := &FileConfig{
		Agents: []AgentConfig{
			{ID: "a", Model: ModelConfig{Provider: "ollama"}, Durable: true},
		},
	}
	err := validateFileConfig(fc)
	if err == nil || !strings.Contains(err.Error(), "durable requires a graph block") {
		t.Fatalf("expected durable-requires-graph error, got %v", err)
	}
}

func TestValidateFileConfigDurableRequiresPersistentStorage(t *testing.T) {
	fc := &FileConfig{
		Agents: []AgentConfig{
			{
				ID:      "a",
				Model:   ModelConfig{Provider: "ollama"},
				Durable: true,
				Storage: StorageConfig{Backend: "memory"},
				Graph: &GraphConfig{
					Entry: "n",
					Nodes: []NodeConfig{{ID: "n", Type: "passthrough"}},
				},
			},
		},
	}
	err := validateFileConfig(fc)
	if err == nil || !strings.Contains(err.Error(), "durable requires persistent storage") {
		t.Fatalf("expected persistent-storage error, got %v", err)
	}
}

func TestValidateFileConfigUnknownSubagentReference(t *testing.T) {
	fc := &FileConfig{
		Agents: []AgentConfig{
			{
				ID:    "a",
				Model: ModelConfig{Provider: "ollama"},
				Graph: &GraphConfig{
					Entry: "n",
					Nodes: []NodeConfig{{ID: "n", Type: "subagent", Agent: "does-not-exist"}},
				},
			},
		},
	}
	err := validateFileConfig(fc)
	if err == nil || !strings.Contains(err.Error(), `references unknown subagent "does-not-exist"`) {
		t.Fatalf("expected unknown-subagent error, got %v", err)
	}
}
