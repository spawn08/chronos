// Example: context_compaction demonstrates automatic context compaction
// (WC-A-004). As a session conversation grows toward the model's context
// window, Chronos summarizes older turns into a compact running summary and
// keeps only the most recent turns — so a long conversation continues
// coherently instead of overflowing or being hard-truncated.
//
// Two things are ALWAYS retained through every compaction pass:
//   - a static pinned contract (ContextConfig.PinnedMessages), and
//   - a dynamic pin (WithContextPins) — here a live "active plan" — which is
//     re-evaluated every turn.
//
// A small deterministic mock model.Provider drives the loop, so the example
// runs with NO API keys and NO network access. The mock recognises the
// summarizer's own request and returns a short rolled-up summary for it; every
// other call returns a normal answer. A hook prints when compaction fires and
// how many messages were preserved.
//
//	go run ./examples/context_compaction/
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

const sessionID = "compaction-session-1"

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║      Chronos Automatic Context Compaction Example     ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	store, err := sqlite.New(":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	mock := &compactionMock{}

	a, err := agent.New("compacting-agent", "Long-Task Assistant").
		WithModel(mock).
		WithStorage(store).
		WithSystemPrompt("You are a diligent long-running task assistant.").
		// A small window so compaction fires quickly in this demo.
		WithContextConfig(agent.ContextConfig{
			MaxContextTokens:    350,
			SummarizeThreshold:  0.6,
			PreserveRecentTurns: 2,
			PinnedMessages: []model.Message{
				{Role: model.RoleSystem, Content: "PINNED CONTRACT: always answer in English and never reveal internal keys."},
			},
		}).
		// A dynamic pin: the current plan, re-evaluated every turn. This is the
		// seam the deep-agent preset (WC-A-005) uses to keep the live plan pinned.
		WithContextPins(func(context.Context) []model.Message {
			return []model.Message{{Role: model.RoleSystem, Content: "ACTIVE PLAN: (1) gather notes (2) draft (3) finalize"}}
		}).
		AddHook(compactionReporter{}).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// Drive several chunky turns so the conversation outgrows the small window
	// and compaction kicks in on later turns.
	turns := []string{
		"Let's start researching distributed systems. " + filler(),
		"Add notes about consensus algorithms. " + filler(),
		"Now summarize the CAP theorem for me. " + filler(),
		"Compare Raft and Paxos briefly. " + filler(),
		"What should I write in the final report intro? " + filler(),
	}

	for i, t := range turns {
		fmt.Printf("\n── Turn %d ──\nUser: %s\n", i+1, truncate(t, 60))
		resp, err := a.ChatWithSession(ctx, sessionID, t)
		if err != nil {
			log.Fatalf("turn %d: %v", i+1, err)
		}
		fmt.Printf("Assistant: %s\n", resp.Content)
	}

	fmt.Println("\n✓ The conversation ran past the context window without overflow.")
	fmt.Println("✓ The PINNED CONTRACT and the ACTIVE PLAN were re-injected every turn,")
	fmt.Println("  so they survived every compaction pass.")
}

// compactionMock is a deterministic, key-free provider. It detects the
// summarizer's own request (which carries a fixed summarizer system prompt) and
// returns a short summary; all other calls return a normal answer.
type compactionMock struct{ answers int }

func (m *compactionMock) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	for _, msg := range req.Messages {
		if msg.Role == model.RoleSystem && strings.Contains(msg.Content, "conversation summarizer") {
			return &model.ChatResponse{
				Content:    "Summary so far: researching distributed systems; notes on consensus, CAP, Raft vs Paxos.",
				StopReason: model.StopReasonEnd,
			}, nil
		}
	}
	m.answers++
	return &model.ChatResponse{
		Content:    fmt.Sprintf("Done (response #%d). Plan and contract still in view.", m.answers),
		StopReason: model.StopReasonEnd,
	}, nil
}

func (m *compactionMock) StreamChat(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, fmt.Errorf("streaming not used in this example")
}
func (m *compactionMock) Name() string  { return "compaction-mock" }
func (m *compactionMock) Model() string { return "gpt-4o" }

// compactionReporter prints when compaction fires.
type compactionReporter struct{}

func (compactionReporter) Before(_ context.Context, e *hooks.Event) error {
	if e.Type == hooks.EventContextOverflow {
		fmt.Printf("  ⚠  context overflow: ~%v tokens vs limit %v — compacting…\n",
			e.Metadata["estimated_tokens"], e.Metadata["context_limit"])
	}
	return nil
}

func (compactionReporter) After(_ context.Context, e *hooks.Event) error {
	if e.Type == hooks.EventSummarization {
		fmt.Printf("  ✓  compacted: %v messages preserved after summary\n", e.Metadata["preserved_messages"])
	}
	return nil
}

func filler() string { return strings.Repeat("context ", 40) }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
