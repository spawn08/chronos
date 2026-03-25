// Example: graph_with_llm — StateGraph with real LLM calls inside nodes.
//
// What you'll learn:
//   - How to wire a real LLM provider (OpenAI, Anthropic, Gemini, Ollama) into graph nodes
//   - Building a multi-step workflow where each node calls the LLM for reasoning
//   - Conditional routing based on LLM classification output
//   - Using tools inside graph nodes for grounded agent behavior
//   - Checkpointing and resume with SQLite persistence
//
// Prerequisites:
//   - Go 1.22+
//   - At least one LLM API key (or Ollama running locally)
//
// Environment variables (set ONE of these):
//
//	OPENAI_API_KEY=sk-...               → uses GPT-4o
//	ANTHROPIC_API_KEY=sk-ant-...        → uses Claude Sonnet 4
//	GEMINI_API_KEY=AIza...              → uses Gemini 2.0 Flash
//	(none)                              → uses Ollama at localhost:11434
//
// Run:
//
//	export OPENAI_API_KEY=sk-your-key
//	go run ./examples/graph_with_llm/
//
// YAML equivalent (same behavior, no Go code):
//
//	See examples/yaml-configs/graph-agent.yaml
//	CHRONOS_CONFIG=examples/yaml-configs/graph-agent.yaml go run ./cli/main.go run "Explain goroutines"
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
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos: StateGraph with Real LLM Calls        ║")
	fmt.Println("╚═══════════════════════════════════════════════════╝")

	// ════════════════════════════════════════════════════════════════
	// Step 1: Choose your LLM provider
	//
	// Chronos supports 14+ providers. Set ONE environment variable
	// and the agent connects automatically. Every provider implements
	// the same Provider interface, so your graph code never changes.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 1: LLM Provider ━━━")

	provider, providerName := resolveProvider()
	fmt.Printf("  Provider: %s\n", providerName)
	fmt.Printf("  Model:    %s\n", provider.Model())

	// ════════════════════════════════════════════════════════════════
	// Step 2: Set up persistent storage
	//
	// SQLite stores sessions, checkpoints, and events. Every graph
	// node execution is checkpointed — if the process crashes, you
	// can resume from the last completed node.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 2: Storage ━━━")

	store, err := sqlite.New(":memory:")
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	fmt.Println("  SQLite in-memory store ready")

	// ════════════════════════════════════════════════════════════════
	// Step 3: Register tools the LLM can call
	//
	// Tools are functions the LLM can invoke during conversation.
	// Define the JSON Schema for arguments so the model knows
	// what parameters to provide.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 3: Tool Registry ━━━")

	toolRegistry := tool.NewRegistry()

	toolRegistry.Register(&tool.Definition{
		Name:        "search_docs",
		Description: "Search technical documentation for a Go concept. Returns relevant documentation snippets.",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The Go concept to search for (e.g., 'goroutines', 'channels', 'interfaces')",
				},
			},
			"required": []string{"query"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			query, _ := args["query"].(string)
			docs := map[string]string{
				"goroutines": "Goroutines are lightweight threads managed by the Go runtime. They start with the 'go' keyword. Unlike OS threads, goroutines use only ~2KB of stack space and are multiplexed onto a smaller number of OS threads by the Go scheduler.",
				"channels":   "Channels are typed conduits for sending and receiving values between goroutines. They provide synchronization without explicit locks. Use 'make(chan Type)' to create, '<-' to send/receive.",
				"interfaces": "Interfaces in Go are satisfied implicitly — any type that implements all methods of an interface automatically satisfies it. This enables duck typing with compile-time safety.",
				"context":    "context.Context carries deadlines, cancellation signals, and request-scoped values across API boundaries. Always pass it as the first parameter. Use context.WithTimeout or context.WithCancel for lifecycle management.",
			}
			queryLower := strings.ToLower(query)
			for key, doc := range docs {
				if strings.Contains(queryLower, key) {
					return map[string]any{"found": true, "content": doc, "topic": key}, nil
				}
			}
			return map[string]any{"found": false, "content": "No documentation found for: " + query}, nil
		},
	})

	fmt.Printf("  Registered %d tools\n", len(toolRegistry.List()))

	// ════════════════════════════════════════════════════════════════
	// Step 4: Build the StateGraph with LLM-powered nodes
	//
	// This graph implements a 3-stage pipeline:
	//   classify → (technical|general) → respond
	//
	// The classifier node calls the LLM to determine the question
	// type. A conditional edge routes to the appropriate handler.
	// The respond node calls the LLM again with tools for grounded answers.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 4: StateGraph Construction ━━━")

	g := graph.New("llm-graph")

	// --- Node 1: Classify the user's question using the LLM ---
	g.AddNode("classify", func(ctx context.Context, s graph.State) (graph.State, error) {
		question, _ := s["question"].(string)
		fmt.Printf("  [classify] Asking LLM to classify: %q\n", truncate(question, 60))

		resp, err := provider.Chat(ctx, &model.ChatRequest{
			Messages: []model.Message{
				{Role: model.RoleSystem, Content: `You are a classifier. Given a question, respond with EXACTLY one word:
- "technical" if the question is about programming, Go, software, or computer science
- "general" if the question is about anything else

Respond with only the single word, nothing else.`},
				{Role: model.RoleUser, Content: question},
			},
		})
		if err != nil {
			return s, fmt.Errorf("classify: %w", err)
		}

		category := strings.TrimSpace(strings.ToLower(resp.Content))
		if category != "technical" && category != "general" {
			category = "general"
		}

		s["category"] = category
		fmt.Printf("  [classify] Category: %s (tokens: %d+%d)\n",
			category, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
		return s, nil
	})

	// --- Node 2a: Handle technical questions with tools ---
	g.AddNode("technical", func(ctx context.Context, s graph.State) (graph.State, error) {
		question, _ := s["question"].(string)
		fmt.Printf("  [technical] Answering with tools: %q\n", truncate(question, 60))

		req := &model.ChatRequest{
			Messages: []model.Message{
				{Role: model.RoleSystem, Content: `You are a Go programming expert. Use the search_docs tool to find relevant documentation before answering. Provide clear, accurate answers with code examples when helpful.`},
				{Role: model.RoleUser, Content: question},
			},
		}
		for _, t := range toolRegistry.List() {
			req.Tools = append(req.Tools, model.ToolDefinition{
				Type: "function",
				Function: model.FunctionDef{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			})
		}

		resp, err := provider.Chat(ctx, req)
		if err != nil {
			return s, fmt.Errorf("technical: %w", err)
		}

		// Handle tool calls if the LLM requests them
		if resp.StopReason == model.StopReasonToolCall && len(resp.ToolCalls) > 0 {
			messages := req.Messages
			messages = append(messages, model.Message{
				Role:      model.RoleAssistant,
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

			for _, tc := range resp.ToolCalls {
				fmt.Printf("  [technical] Tool call: %s\n", tc.Name)
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Arguments), &args)

				result, toolErr := toolRegistry.Execute(ctx, tc.Name, args)
				content := ""
				if toolErr != nil {
					content = fmt.Sprintf("Error: %s", toolErr.Error())
				} else {
					resultJSON, _ := json.Marshal(result)
					content = string(resultJSON)
				}

				messages = append(messages, model.Message{
					Role:       model.RoleTool,
					Content:    content,
					ToolCallID: tc.ID,
					Name:       tc.Name,
				})
			}

			resp, err = provider.Chat(ctx, &model.ChatRequest{Messages: messages})
			if err != nil {
				return s, fmt.Errorf("technical follow-up: %w", err)
			}
		}

		s["response"] = resp.Content
		s["source"] = "technical-with-tools"
		return s, nil
	})

	// --- Node 2b: Handle general questions directly ---
	g.AddNode("general", func(ctx context.Context, s graph.State) (graph.State, error) {
		question, _ := s["question"].(string)
		fmt.Printf("  [general] Answering directly: %q\n", truncate(question, 60))

		resp, err := provider.Chat(ctx, &model.ChatRequest{
			Messages: []model.Message{
				{Role: model.RoleSystem, Content: "You are a helpful assistant. Give concise, accurate answers."},
				{Role: model.RoleUser, Content: question},
			},
		})
		if err != nil {
			return s, fmt.Errorf("general: %w", err)
		}

		s["response"] = resp.Content
		s["source"] = "general-knowledge"
		return s, nil
	})

	// --- Wire the graph: classify → conditional → respond ---
	g.SetEntryPoint("classify")
	g.AddConditionalEdge("classify", func(s graph.State) string {
		category, _ := s["category"].(string)
		if category == "technical" {
			return "technical"
		}
		return "general"
	})
	g.SetFinishPoint("technical")
	g.SetFinishPoint("general")

	fmt.Println("  Graph: classify → [technical|general] → END")
	fmt.Println("  Conditional routing based on LLM classification")

	// ════════════════════════════════════════════════════════════════
	// Step 5: Build the agent and run it
	//
	// The agent wraps the graph with storage for checkpointing.
	// Each node execution is persisted — you can resume after crashes.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 5: Agent Execution ━━━")

	a, err := agent.New("graph-llm-agent", "Graph LLM Agent").
		WithStorage(store).
		WithGraph(g).
		Build()
	if err != nil {
		log.Fatalf("build agent: %v", err)
	}

	// --- Run a technical question ---
	fmt.Println("\n  Question 1 (technical):")
	result, err := a.Run(ctx, map[string]any{
		"question": "What are goroutines in Go and how do they differ from OS threads?",
	})
	if err != nil {
		log.Fatalf("run: %v", err)
	}
	printResult(result)

	// --- Run a general question ---
	fmt.Println("\n  Question 2 (general):")
	result, err = a.Run(ctx, map[string]any{
		"question": "What is the tallest mountain in the world?",
	})
	if err != nil {
		log.Fatalf("run: %v", err)
	}
	printResult(result)

	fmt.Println("\n✓ Graph with LLM example completed.")
	fmt.Println("\n  YAML equivalent: See examples/yaml-configs/graph-agent.yaml")
	fmt.Println("  Run: CHRONOS_CONFIG=examples/yaml-configs/graph-agent.yaml go run ./cli/main.go run \"Your question\"")
}

func printResult(result *graph.RunState) {
	fmt.Printf("  Status:   %s\n", result.Status)
	fmt.Printf("  Category: %v\n", result.State["category"])
	fmt.Printf("  Source:   %v\n", result.State["source"])
	response, _ := result.State["response"].(string)
	if len(response) > 300 {
		response = response[:300] + "..."
	}
	fmt.Printf("  Response: %s\n", response)
}

func resolveProvider() (model.Provider, string) {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return model.NewOpenAI(key), "OpenAI"
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return model.NewAnthropic(key), "Anthropic"
	}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		return model.NewGemini(key), "Gemini"
	}
	fmt.Println("  No cloud API key found — using Ollama (localhost:11434)")
	fmt.Println("  Start Ollama: ollama serve && ollama pull llama3.2")
	return model.NewOllama("http://localhost:11434", "llama3.2"), "Ollama (local)"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
