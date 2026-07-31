package harness

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool/builtins"
	"github.com/spawn08/chronos/storage"
	memstore "github.com/spawn08/chronos/storage/adapters/memory"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

const deepResearcherPrompt = "You are a focused researcher. Return one concise finding."

// deepMock drives both the deep-agent role (a scripted multi-step sequence) and
// the subagent role (routed by its own system prompt). It records the system
// context sent on each deep-agent turn so tests can assert the pinned plan is
// present. It is deterministic and key-free.
type deepMock struct {
	mu               sync.Mutex
	step             int
	deepAgentSystems [][]string // system-message contents captured per deep-agent turn
	subagentRan      bool       // set when the subagent role executed
}

func (m *deepMock) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	// Subagent role: its distinct prompt is present. Return only a finding.
	for i := range req.Messages {
		if req.Messages[i].Role == model.RoleSystem && req.Messages[i].Content == deepResearcherPrompt {
			m.mu.Lock()
			m.subagentRan = true
			m.mu.Unlock()
			return end("Go was released by Google in 2009."), nil
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Capture the system context this deep-agent turn saw.
	var systems []string
	for i := range req.Messages {
		if req.Messages[i].Role == model.RoleSystem {
			systems = append(systems, req.Messages[i].Content)
		}
	}
	m.deepAgentSystems = append(m.deepAgentSystems, systems)

	step := m.step
	m.step++
	switch step {
	case 0: // turn 1: lay out the plan
		return toolCall("c-plan-1", builtins.PlanToolName, map[string]any{
			"tasks": []any{
				map[string]any{"content": "research the topic", "status": "in_progress"},
				map[string]any{"content": "write the report", "status": "pending"},
			},
		}), nil
	case 1: // turn 1: finish the turn (the plan is now persisted)
		return end("Plan ready; I'll continue next turn."), nil
	case 2: // turn 2: offload notes to the virtual filesystem
		return toolCall("c-fs-1", builtins.FSWriteToolName, map[string]any{
			"path":    "research/notes.md",
			"content": strings.Repeat("finding ", 100),
		}), nil
	case 3: // turn 2: delegate research to a subagent
		return toolCall("c-spawn-1", SpawnToolName, map[string]any{
			"agent": "researcher",
			"task":  "One-line history of Go.",
		}), nil
	case 4: // turn 2: mark the plan complete
		return toolCall("c-plan-2", builtins.PlanToolName, map[string]any{
			"tasks": []any{
				map[string]any{"content": "research the topic", "status": "completed"},
				map[string]any{"content": "write the report", "status": "completed"},
			},
		}), nil
	default: // turn 2: finish
		return end("Final report: Go was released by Google in 2009."), nil
	}
}

func (m *deepMock) StreamChat(ctx context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, err := m.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *deepMock) Name() string  { return "deep-mock" }
func (m *deepMock) Model() string { return "gpt-4o" }

func end(content string) *model.ChatResponse {
	return &model.ChatResponse{Role: model.RoleAssistant, Content: content, StopReason: model.StopReasonEnd}
}

func toolCall(id, name string, args map[string]any) *model.ChatResponse {
	raw, _ := json.Marshal(args)
	return &model.ChatResponse{
		Role:       model.RoleAssistant,
		StopReason: model.StopReasonToolCall,
		ToolCalls:  []model.ToolCall{{ID: id, Name: name, Arguments: string(raw)}},
	}
}

func TestNewDeepAgent_RequiresModel(t *testing.T) {
	if _, err := NewDeepAgent(DeepAgentConfig{}); err == nil {
		t.Fatal("expected error when model is nil")
	}
}

func TestNewDeepAgent_WiresHarnessTools(t *testing.T) {
	tests := []struct {
		name             string
		disableSubAgents bool
		wantTools        []string
		absentTools      []string
	}{
		{
			name:      "full harness",
			wantTools: []string{builtins.PlanToolName, builtins.FSWriteToolName, builtins.FSReadToolName, SpawnToolName},
		},
		{
			name:             "subagents disabled",
			disableSubAgents: true,
			wantTools:        []string{builtins.PlanToolName, builtins.FSWriteToolName},
			absentTools:      []string{SpawnToolName},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := NewDeepAgent(DeepAgentConfig{
				Model:            &deepMock{},
				DisableSubAgents: tt.disableSubAgents,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range tt.wantTools {
				if _, ok := a.Tools.Get(name); !ok {
					t.Errorf("expected tool %q to be registered", name)
				}
			}
			for _, name := range tt.absentTools {
				if _, ok := a.Tools.Get(name); ok {
					t.Errorf("tool %q should not be registered", name)
				}
			}
		})
	}
}

func TestNewDeepAgent_StorageWithoutFileStoreFails(t *testing.T) {
	// The in-memory storage adapter implements storage.Storage but not
	// storage.SessionFileStore, so the durable VFS cannot be constructed.
	if _, err := NewDeepAgent(DeepAgentConfig{
		Model:   &deepMock{},
		Storage: memstore.New(),
	}); err == nil {
		t.Fatal("expected construction to fail without SessionFileStore")
	}
}

func TestNewDeepAgent_EndToEnd(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if migErr := store.Migrate(ctx); migErr != nil {
		t.Fatal(migErr)
	}

	mock := &deepMock{}
	a, err := NewDeepAgent(DeepAgentConfig{
		Model:   mock,
		Storage: store,
		SubAgents: []SubAgentSpec{
			{Name: "researcher", Description: "Researches a topic.", SystemPrompt: deepResearcherPrompt},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	sid := "deep-e2e"
	// Turn 1 lays out the plan; turn 2 executes it. Splitting across turns is the
	// realistic long-task flow and lets the second turn start with the plan pinned.
	if _, t1Err := a.ChatWithSession(ctx, sid, "Plan how to research Go's history and write a report."); t1Err != nil {
		t.Fatalf("ChatWithSession turn 1: %v", t1Err)
	}
	resp, err := a.ChatWithSession(ctx, sid, "Now carry out the plan.")
	if err != nil {
		t.Fatalf("ChatWithSession turn 2: %v", err)
	}
	if resp == nil || !strings.Contains(resp.Content, "Final report") {
		t.Fatalf("unexpected final response: %+v", resp)
	}

	// Planning: the plan was persisted and ended complete.
	planStore := builtins.NewStoragePlanStore(store)
	sctx := storage.WithSession(ctx, sid)
	plan, err := planStore.Load(sctx)
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}
	if !plan.Complete() {
		t.Errorf("plan should be complete, got %s", plan.Summary())
	}

	// Offloading: the artifact is in the VFS, not lost.
	vfs, err := builtins.NewStorageVFS(store)
	if err != nil {
		t.Fatal(err)
	}
	data, err := vfs.Read(sctx, "research/notes.md")
	if err != nil {
		t.Fatalf("read offloaded artifact: %v", err)
	}
	if len(data) == 0 {
		t.Error("offloaded artifact is empty")
	}

	// Delegation: the subagent role actually executed during the spawn_subagent call.
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if !mock.subagentRan {
		t.Error("subagent was never invoked via spawn_subagent")
	}

	// Compaction seam: once the plan existed, later turns pinned it in the system
	// context so it survives summarization.
	pinnedOnce := false
	for _, systems := range mock.deepAgentSystems {
		for _, s := range systems {
			if strings.Contains(s, "Current plan") {
				pinnedOnce = true
			}
		}
	}
	if !pinnedOnce {
		t.Error("active plan was never pinned into the system context")
	}
}

// TestNewDeepAgent_StoragelessChat exercises the ephemeral (no Storage) mode:
// the in-memory plan/VFS tools still work, but the caller must supply a
// session-scoped context (compaction is unavailable without storage).
func TestNewDeepAgent_StoragelessChat(t *testing.T) {
	a, err := NewDeepAgent(DeepAgentConfig{Model: &deepMock{}})
	if err != nil {
		t.Fatal(err)
	}
	// Storageless mode still needs a session scope for the plan/VFS tools.
	ctx := storage.WithSession(context.Background(), "storageless-1")
	resp, err := a.Chat(ctx, "Plan a tiny task.")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	// deepMock step 0 calls update_plan (against the in-memory store); a
	// successful reply proves the tool ran without a storage backend.
	if resp == nil || !strings.Contains(resp.Content, "Plan ready") {
		t.Fatalf("unexpected storageless response: %+v", resp)
	}
}

func TestNewDeepAgent_ContradictorySubAgentConfig(t *testing.T) {
	_, err := NewDeepAgent(DeepAgentConfig{
		Model:            &deepMock{},
		DisableSubAgents: true,
		SubAgents:        []SubAgentSpec{{Name: "x", SystemPrompt: "y"}},
	})
	if err == nil {
		t.Fatal("expected error for DisableSubAgents + SubAgents")
	}
}
