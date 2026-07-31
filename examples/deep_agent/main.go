// Example: deep_agent demonstrates the batteries-included "deep agent" harness
// preset (WC-A-005). A single harness.NewDeepAgent(...) call assembles the whole
// agent harness — planning (WC-A-001), a virtual filesystem for context
// offloading (WC-A-002), context-isolated subagents (WC-A-003), automatic
// compaction with the active plan pinned (WC-A-004), and (optionally) semantic
// memory recall (WC-D-001) — with a sensible default prompt and tool set, and no
// extra wiring.
//
// A single deterministic mock model.Provider drives every role (the deep agent
// and its subagent, which inherits the same model), so the example runs with NO
// API keys and NO network. It works a realistic long task across two turns:
//
//	turn 1: lay out a plan with update_plan
//	turn 2: offload notes with fs_write, delegate research with spawn_subagent,
//	        mark the plan complete, and produce the report
//
// Because storage is durable (sqlite), the plan and files survive across turns,
// and the active plan is pinned into context so compaction never drops it.
//
//	go run ./examples/deep_agent/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool/builtins"
	"github.com/spawn08/chronos/sdk/harness"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

const researcherPrompt = "You are a focused researcher. Return one concise finding."
const sessionID = "deep-agent-session-1"

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║          Chronos Deep Agent Preset Example            ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	store, err := sqlite.New(":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	// One call wires the entire harness. Everything is override-able via the
	// config; here we only register a specialist subagent.
	a, err := harness.NewDeepAgent(harness.DeepAgentConfig{
		ID:      "researcher-agent",
		Name:    "Research Assistant",
		Model:   &deepMock{},
		Storage: store,
		SubAgents: []harness.SubAgentSpec{
			{Name: "researcher", Description: "Researches a topic and returns a concise finding.", SystemPrompt: researcherPrompt},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	turns := []string{
		"Plan how to research the history of Go and write a short report.",
		"Now carry out the plan.",
	}
	for i, t := range turns {
		fmt.Printf("\n── Turn %d ──\nUser: %s\n", i+1, t)
		resp, err := a.ChatWithSession(ctx, sessionID, t)
		if err != nil {
			log.Fatalf("turn %d: %v", i+1, err)
		}
		fmt.Printf("Agent: %s\n", resp.Content)
	}

	// Show the durable side effects the harness produced.
	sctx := storage.WithSession(ctx, sessionID)
	plan, err := builtins.NewStoragePlanStore(store).Load(sctx)
	if err != nil {
		log.Fatalf("load plan: %v", err)
	}
	fmt.Printf("\nFinal plan (complete=%v):\n%s\n", plan.Complete(), plan.Summary())

	vfs, _ := builtins.NewStorageVFS(store)
	if data, err := vfs.Read(sctx, "research/notes.md"); err == nil {
		fmt.Printf("\nOffloaded artifact research/notes.md: %d bytes kept out of context\n", len(data))
	}

	fmt.Println("\n✓ One NewDeepAgent call gave the agent planning, offloading, delegation,")
	fmt.Println("  and compaction — the plan was pinned so it survived every turn.")
}

// deepMock plays the deep agent (a scripted multi-step sequence) and its subagent
// (routed by the researcher prompt). Deterministic and key-free.
type deepMock struct{ step int }

func (m *deepMock) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	for _, msg := range req.Messages {
		if msg.Role == model.RoleSystem && msg.Content == researcherPrompt {
			fmt.Printf("  [researcher] fresh context of %d messages; returning a finding\n", len(req.Messages))
			return end("Go was created at Google (2007) and released in 2009."), nil
		}
	}

	step := m.step
	m.step++
	switch step {
	case 0:
		fmt.Println("  [agent] writing a plan")
		return toolCall("c1", builtins.PlanToolName, map[string]any{
			"tasks": []any{
				map[string]any{"content": "research the topic", "status": "in_progress"},
				map[string]any{"content": "write the report", "status": "pending"},
			},
		}), nil
	case 1:
		return end("Plan ready; I'll execute it next turn."), nil
	case 2:
		fmt.Println("  [agent] offloading notes to the virtual filesystem")
		return toolCall("c2", builtins.FSWriteToolName, map[string]any{
			"path":    "research/notes.md",
			"content": strings.Repeat("research finding ", 200),
		}), nil
	case 3:
		fmt.Println("  [agent] delegating to the researcher subagent")
		return toolCall("c3", harness.SpawnToolName, map[string]any{
			"agent": "researcher",
			"task":  "One-line history of Go.",
		}), nil
	case 4:
		fmt.Println("  [agent] marking the plan complete")
		return toolCall("c4", builtins.PlanToolName, map[string]any{
			"tasks": []any{
				map[string]any{"content": "research the topic", "status": "completed"},
				map[string]any{"content": "write the report", "status": "completed"},
			},
		}), nil
	default:
		return end("Final report: Go was created at Google in 2007 and released in 2009."), nil
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
