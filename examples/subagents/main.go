// Example: subagents demonstrates context-isolated delegation (WC-A-003). A
// parent agent delegates a research sub-task to a subagent with spawn_subagent.
// The subagent works in its OWN fresh conversation and returns only its final
// result — its intermediate reasoning never enters the parent's context window.
//
// A single deterministic mock model.Provider drives both roles (subagents
// inherit the parent's model), routing on the system prompt, so the example runs
// with NO API keys and NO network:
//
//	parent turn 0: delegate "research Go's history" via spawn_subagent
//	  subagent:    (fresh context) produce a one-line finding
//	parent turn 1: answer using only the returned finding
//
//	go run ./examples/subagents/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/harness"
)

const researcherPrompt = "You are a focused researcher. Answer in one sentence."

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║   Chronos Context-Isolated Subagents Example           ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	parent, err := agent.New("lead", "Lead Agent").
		WithModel(&routingMock{}).
		WithSystemPrompt("You coordinate work and delegate research to subagents.").
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// Derive a subagent service from the built parent and register a specialist.
	svc, err := harness.NewSubAgentService(parent)
	if err != nil {
		log.Fatal(err)
	}
	if err := svc.Register(harness.SubAgentSpec{
		Name:         "researcher",
		Description:  "Researches a topic and returns a concise finding.",
		SystemPrompt: researcherPrompt,
	}); err != nil {
		log.Fatal(err)
	}
	// Delegation runs in-process here; pass harness.NewQueuedRunner(...) instead
	// for durable, resumable delegation across workers.
	harness.Attach(svc, harness.NewInProcessRunner(svc))

	task := "Give me a one-line history of the Go programming language."
	fmt.Printf("\nUser: %s\n\n", task)

	resp, err := parent.Chat(ctx, task)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nLead: %s\n", resp.Content)
	fmt.Println("\n✓ The researcher worked in an isolated context; the lead saw only its final finding.")
}

// routingMock plays both the lead and the researcher, deciding from the system
// prompt and the latest message. It records nothing global; each call is pure.
type routingMock struct{}

func (m *routingMock) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	// Researcher role: its own system prompt is present.
	for _, msg := range req.Messages {
		if msg.Role == model.RoleSystem && msg.Content == researcherPrompt {
			fmt.Printf("  [researcher] fresh context of %d messages; producing a finding\n", len(req.Messages))
			return end("Go was created at Google in 2007 by Griesemer, Pike, and Thompson and released in 2009."), nil
		}
	}

	// Lead role: after the delegation returns, answer using the finding.
	last := req.Messages[len(req.Messages)-1]
	if last.Role == model.RoleTool {
		var out struct {
			Result string `json:"result"`
		}
		_ = json.Unmarshal([]byte(last.Content), &out)
		fmt.Printf("  [lead] received only the subagent's %d-char result (not its reasoning)\n", len(out.Result))
		return end("Here's what my researcher found: " + out.Result), nil
	}

	// Lead role, first turn: delegate to the researcher.
	return toolCall("call_1", harness.SpawnToolName, map[string]any{
		"agent": "researcher",
		"task":  "One-line history of Go.",
	}), nil
}

func (m *routingMock) StreamChat(ctx context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, err := m.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *routingMock) Name() string  { return "routing-mock" }
func (m *routingMock) Model() string { return "mock-v1" }

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
