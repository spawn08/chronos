// Example: memory_and_sessions demonstrates persistent multi-turn sessions
// and the memory system (short-term + long-term).
//
// No API keys needed — runs entirely with SQLite and a mock provider.
//
//	go run ./examples/memory_and_sessions/
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/memory"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
	ctx := context.Background()

	store, err := sqlite.New(":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos Memory & Sessions Example                 ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	provider := &sessionMockProvider{turnCount: 0}

	// ── 1. Memory Store — direct API ──

	fmt.Println("\n━━━ 1. Memory Store (Direct API) ━━━")

	memStore := memory.NewStore("assistant", store)

	if err := memStore.SetShortTerm(ctx, "session-1", "current_topic", "renewable energy"); err != nil {
		log.Fatal(err)
	}
	if err := memStore.SetShortTerm(ctx, "session-1", "user_mood", "curious"); err != nil {
		log.Fatal(err)
	}
	if err := memStore.SetLongTerm(ctx, "user_name", "Alice"); err != nil {
		log.Fatal(err)
	}
	if err := memStore.SetLongTerm(ctx, "preferred_language", "English"); err != nil {
		log.Fatal(err)
	}
	if err := memStore.SetLongTerm(ctx, "expertise_level", "intermediate"); err != nil {
		log.Fatal(err)
	}

	fmt.Println("  Short-term memories (session-1):")
	shortTerm, err := memStore.ListShortTerm(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range shortTerm {
		fmt.Printf("    %-20s = %v  (session: %s)\n", m.Key, m.Value, m.SessionID)
	}

	fmt.Println("  Long-term memories:")
	longTerm, err := memStore.ListLongTerm(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range longTerm {
		fmt.Printf("    %-20s = %v\n", m.Key, m.Value)
	}

	val, err := memStore.Get(ctx, "user_name")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  Direct lookup (user_name): %v\n", val)

	// ── 2. Multi-turn sessions with ChatWithSession ──

	fmt.Println("\n━━━ 2. Multi-Turn Sessions (ChatWithSession) ━━━")

	a, err := agent.New("session-agent", "Session Agent").
		WithModel(provider).
		WithStorage(store).
		WithMemory(memStore).
		WithSystemPrompt("You are a helpful assistant. Remember context from the conversation.").
		Build()
	if err != nil {
		log.Fatal(err)
	}

	sessionID := "demo-session-001"
	conversation := []string{
		"Hi, my name is Alice. I'm interested in Go programming.",
		"What are goroutines and how do they work?",
		"Can you give me an example of channels?",
		"How does the select statement work with channels?",
		"Thanks! That was very helpful.",
	}

	for i, msg := range conversation {
		fmt.Printf("\n  Turn %d:\n", i+1)
		fmt.Printf("    User:      %s\n", msg)

		resp, err := a.ChatWithSession(ctx, sessionID, msg)
		if err != nil {
			fmt.Printf("    Error:     %v\n", err)
			continue
		}
		content := resp.Content
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		fmt.Printf("    Assistant: %s\n", content)
	}

	// ── 3. Verify session persistence ──

	fmt.Println("\n━━━ 3. Session Persistence ━━━")

	sessions, err := store.ListSessions(ctx, "session-agent", 10, 0)
	if err != nil {
		fmt.Printf("  Error listing sessions: %v\n", err)
	} else {
		fmt.Printf("  Active sessions: %d\n", len(sessions))
		for _, s := range sessions {
			fmt.Printf("    ID: %s  Agent: %s  Status: %s\n", s.ID, s.AgentID, s.Status)
		}
	}

	events, err := store.ListEvents(ctx, sessionID, 0)
	if err != nil {
		fmt.Printf("  Error getting events: %v\n", err)
	} else {
		fmt.Printf("  Events in session %q: %d\n", sessionID, len(events))
		for _, e := range events {
			payload := ""
			if p, ok := e.Payload.(map[string]any); ok {
				if role, ok := p["role"].(string); ok {
					content, _ := p["content"].(string)
					if len(content) > 60 {
						content = content[:60] + "..."
					}
					payload = fmt.Sprintf("%s: %s", role, content)
				}
			}
			fmt.Printf("    seq=%-3d type=%-15s %s\n", e.SeqNum, e.Type, payload)
		}
	}

	// ── 4. Second session for same agent ──

	fmt.Println("\n━━━ 4. Multiple Sessions per Agent ━━━")

	session2ID := "demo-session-002"
	resp, err := a.ChatWithSession(ctx, session2ID, "Hello from session 2! What do you know about Docker?")
	if err != nil {
		fmt.Printf("  Error: %v\n", err)
	} else {
		content := resp.Content
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		fmt.Printf("  Session 2 response: %s\n", content)
	}

	sessions, err = store.ListSessions(ctx, "session-agent", 10, 0)
	if err != nil {
		fmt.Printf("  Error: %v\n", err)
	} else {
		fmt.Printf("  Total sessions for agent: %d\n", len(sessions))
	}

	fmt.Println("\n✓ Memory & Sessions example completed.")
}

type sessionMockProvider struct {
	turnCount int
}

func (p *sessionMockProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	p.turnCount++
	last := req.Messages[len(req.Messages)-1].Content

	responses := map[int]string{
		1: "Nice to meet you, Alice! Go is a great language. What would you like to know?",
		2: "Goroutines are lightweight threads managed by the Go runtime. You launch one with the `go` keyword.",
		3: "Here's a channel example: ch := make(chan int); go func() { ch <- 42 }(); val := <-ch",
		4: "The select statement lets you wait on multiple channel operations simultaneously.",
		5: "You're welcome, Alice! Feel free to ask more anytime.",
	}

	content, ok := responses[p.turnCount]
	if !ok {
		content = fmt.Sprintf("[Turn %d] Response to: %.60s", p.turnCount, last)
	}

	return &model.ChatResponse{
		Content:    content,
		Role:       "assistant",
		StopReason: model.StopReasonEnd,
		Usage: model.Usage{
			PromptTokens:     len(last) / 4,
			CompletionTokens: len(content) / 4,
		},
	}, nil
}

func (p *sessionMockProvider) StreamChat(_ context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, _ := p.Chat(context.Background(), req)
	ch <- resp
	close(ch)
	return ch, nil
}

func (p *sessionMockProvider) Name() string  { return "mock" }
func (p *sessionMockProvider) Model() string { return "mock-v1" }
