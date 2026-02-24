// Example: multi_agent demonstrates all four team strategies and agent communication.
//
// This example shows how lightweight agents (model-only, no graph or storage)
// work together using sequential, parallel, router, and coordinator strategies,
// plus direct agent-to-agent channels and bus-based delegation.
//
// Set OPENAI_API_KEY (or any supported provider key) to run with a real model.
// Without a key, the example runs with a mock provider that echoes prompts.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/protocol"
	"github.com/spawn08/chronos/sdk/team"
)

func main() {
	ctx := context.Background()
	provider := resolveProvider()

	fmt.Println("╔═══════════════════════════════════════════════════╗")
	fmt.Println("║       Chronos Multi-Agent Orchestration Demo     ║")
	fmt.Println("╚═══════════════════════════════════════════════════╝")

	researcher := buildAgent("researcher", "Researcher",
		"Researches topics and gathers facts",
		[]string{"research", "analysis"}, provider,
		"You are a research specialist. Given a topic, provide key facts and findings.")

	writer := buildAgent("writer", "Writer",
		"Writes polished content from research notes",
		[]string{"writing", "content"}, provider,
		"You are a writing specialist. Given research notes, produce polished prose.")

	reviewer := buildAgent("reviewer", "Reviewer",
		"Reviews content for accuracy and quality",
		[]string{"review", "quality"}, provider,
		"You are a reviewer. Evaluate the content for accuracy and quality. Provide feedback.")

	translator := buildAgent("translator", "Translator",
		"Translates content between languages",
		[]string{"translation", "languages"}, provider,
		"You are a translator. Translate the given content into the requested language.")

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 1. Sequential Strategy — pipeline
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	fmt.Println("\n━━━ 1. Sequential Strategy (Researcher → Writer → Reviewer) ━━━")

	seqTeam := team.New("seq-team", "Content Pipeline", team.StrategySequential).
		AddAgent(researcher).
		AddAgent(writer).
		AddAgent(reviewer)

	result, err := seqTeam.Run(ctx, graph.State{
		"message": "Write a short article about renewable energy",
	})
	if err != nil {
		log.Fatalf("Sequential: %v", err)
	}
	fmt.Printf("  Response: %.120s...\n", result["response"])
	fmt.Printf("  Messages exchanged: %d\n", len(seqTeam.MessageHistory()))

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 2. Parallel Strategy — fan-out with bounded concurrency
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	fmt.Println("\n━━━ 2. Parallel Strategy (all agents run concurrently, max 2) ━━━")

	parTeam := team.New("par-team", "Parallel Analysis", team.StrategyParallel).
		AddAgent(researcher).
		AddAgent(writer).
		AddAgent(translator).
		SetMaxConcurrency(2).
		SetErrorStrategy(team.ErrorStrategyBestEffort).
		SetMerge(func(results []graph.State) graph.State {
			merged := make(graph.State)
			for i, r := range results {
				key := fmt.Sprintf("agent_%d_response", i)
				merged[key] = r["response"]
			}
			merged["count"] = len(results)
			return merged
		})

	result, err = parTeam.Run(ctx, graph.State{
		"message": "Summarize the impact of AI on healthcare",
	})
	if err != nil {
		log.Fatalf("Parallel: %v", err)
	}
	fmt.Printf("  Agents completed: %v\n", result["count"])
	for k, v := range result {
		if strings.HasPrefix(k, "agent_") {
			s := fmt.Sprintf("%v", v)
			if len(s) > 100 {
				s = s[:100] + "..."
			}
			fmt.Printf("  %s: %s\n", k, s)
		}
	}

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 3. Router Strategy — intelligent dispatch
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	fmt.Println("\n━━━ 3. Router Strategy (static routing by task type) ━━━")

	routerTeam := team.New("router-team", "Task Router", team.StrategyRouter).
		AddAgent(researcher).
		AddAgent(writer).
		AddAgent(translator).
		SetRouter(func(state graph.State) string {
			msg, _ := state["message"].(string)
			switch {
			case strings.Contains(strings.ToLower(msg), "translate"):
				return "translator"
			case strings.Contains(strings.ToLower(msg), "write"):
				return "writer"
			default:
				return "researcher"
			}
		})

	// Route to translator
	result, err = routerTeam.Run(ctx, graph.State{
		"message": "Translate 'hello world' into French",
	})
	if err != nil {
		log.Fatalf("Router (translate): %v", err)
	}
	fmt.Printf("  Routed to translator: %.100s...\n", result["response"])

	// Route to researcher
	result, err = routerTeam.Run(ctx, graph.State{
		"message": "Research the history of quantum computing",
	})
	if err != nil {
		log.Fatalf("Router (research): %v", err)
	}
	fmt.Printf("  Routed to researcher: %.100s...\n", result["response"])

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 4. Coordinator Strategy — LLM-driven task decomposition
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	fmt.Println("\n━━━ 4. Coordinator Strategy (supervisor decomposes & delegates) ━━━")

	supervisor := buildAgent("supervisor", "Supervisor",
		"Decomposes complex tasks and coordinates specialists",
		[]string{"planning", "coordination"}, provider,
		"You are a project coordinator. Break tasks into sub-tasks and delegate.")

	coordTeam := team.New("coord-team", "Coordinated Team", team.StrategyCoordinator).
		SetCoordinator(supervisor).
		AddAgent(researcher).
		AddAgent(writer).
		AddAgent(reviewer).
		SetMaxIterations(2)

	result, err = coordTeam.Run(ctx, graph.State{
		"message": "Create a report on electric vehicle adoption trends",
	})
	if err != nil {
		fmt.Printf("  Coordinator result: %v (expected with mock provider)\n", err)
	} else {
		fmt.Printf("  Final state keys: %v\n", stateKeys(result))
		fmt.Printf("  Messages exchanged: %d\n", len(coordTeam.MessageHistory()))
	}

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 5. Direct Agent-to-Agent Communication
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	fmt.Println("\n━━━ 5. Direct Agent-to-Agent Channel (bypasses bus) ━━━")

	directTeam := team.New("direct-team", "Direct Comm", team.StrategySequential).
		AddAgent(researcher).
		AddAgent(writer)

	dc := directTeam.DirectChannel("researcher", "writer", 64)

	// Simulate direct point-to-point messaging
	go func() {
		body, _ := json.Marshal(map[string]string{
			"findings": "Solar energy grew 30% in 2025",
		})
		dc.AtoB <- &protocol.Envelope{
			Type:    protocol.TypeTaskResult,
			From:    "researcher",
			To:      "writer",
			Subject: "research_findings",
			Body:    body,
		}
	}()

	received := <-dc.AtoB
	var findings map[string]string
	_ = json.Unmarshal(received.Body, &findings)
	fmt.Printf("  Writer received directly from Researcher: %s\n", findings["findings"])

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 6. Bus-based Task Delegation
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	fmt.Println("\n━━━ 6. Bus-based Task Delegation (researcher delegates to writer) ━━━")

	busTeam := team.New("bus-team", "Bus Delegation", team.StrategySequential).
		AddAgent(researcher).
		AddAgent(writer)

	taskResult, err := busTeam.DelegateTask(ctx, "researcher", "writer", "draft-article",
		protocol.TaskPayload{
			Description: "Write a summary about climate change impacts",
			Input: map[string]any{
				"message": "Write a 2-sentence summary about climate change impacts on agriculture",
			},
		})
	if err != nil {
		log.Fatalf("Delegation: %v", err)
	}
	fmt.Printf("  Delegation success: %v\n", taskResult.Success)
	if resp, ok := taskResult.Output["response"]; ok {
		s := fmt.Sprintf("%v", resp)
		if len(s) > 120 {
			s = s[:120] + "..."
		}
		fmt.Printf("  Writer produced: %s\n", s)
	}

	fmt.Println("\n✓ All strategies demonstrated successfully.")
}

// buildAgent creates a lightweight model-only agent (no graph, no storage).
func buildAgent(id, name, desc string, caps []string, provider model.Provider, systemPrompt string) *agent.Agent {
	b := agent.New(id, name).
		Description(desc).
		WithModel(provider).
		WithSystemPrompt(systemPrompt)

	for _, c := range caps {
		b.AddCapability(c)
	}

	a, err := b.Build()
	if err != nil {
		log.Fatalf("build %s: %v", id, err)
	}
	return a
}

func resolveProvider() model.Provider {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return model.NewOpenAI(key)
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return model.NewAnthropic(key)
	}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		return model.NewGemini(key)
	}
	fmt.Println("⚠ No API key found, using mock provider (set OPENAI_API_KEY for real responses)")
	return &mockProvider{}
}

func stateKeys(s graph.State) []string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	return keys
}

// mockProvider returns the prompt back as the response for demo purposes.
type mockProvider struct{}

func (m *mockProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	last := req.Messages[len(req.Messages)-1].Content
	if strings.Contains(last, "Analyze the following") {
		plan := `{"tasks": [{"agent_id": "researcher", "description": "Research the topic"}, {"agent_id": "writer", "description": "Write the report", "depends_on": "researcher"}], "done": false}`
		return &model.ChatResponse{Content: plan, Role: "assistant", StopReason: model.StopReasonEnd}, nil
	}
	if strings.Contains(last, "Review the results") || strings.Contains(last, "Iteration") {
		return &model.ChatResponse{Content: `{"tasks": [], "done": true}`, Role: "assistant", StopReason: model.StopReasonEnd}, nil
	}
	return &model.ChatResponse{
		Content:    fmt.Sprintf("[Mock response for: %.80s]", last),
		Role:       "assistant",
		StopReason: model.StopReasonEnd,
	}, nil
}

func (m *mockProvider) StreamChat(_ context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, _ := m.Chat(context.Background(), req)
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *mockProvider) Name() string  { return "mock" }
func (m *mockProvider) Model() string { return "mock-v1" }
