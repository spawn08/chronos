// Azure OpenAI tool-calling example for Chronos.
//
// This example shows how to drive multi-round tool calling against an Azure
// OpenAI deployment. The model is given two tools — a calculator and a small
// knowledge lookup — and the example runs the full tool-call loop: it sends
// the request with tool definitions, and whenever the model responds with
// StopReasonToolCall it executes the requested tools, feeds the results back,
// and asks again, repeating until the model produces a final answer.
//
// The same loop pattern works for any provider (see examples/graph_with_llm),
// because tools are wired through the provider-agnostic model.ChatRequest.
//
// Prerequisites:
//   - An Azure OpenAI resource with a chat model deployment (e.g. gpt-4o-mini)
//     that supports function calling
//   - Go 1.24+
//
// Set the following environment variables before running:
//
//	export AZURE_OPENAI_API_KEY=<your-azure-api-key>
//	export AZURE_OPENAI_ENDPOINT=https://<your-resource>.openai.azure.com
//	export AZURE_OPENAI_DEPLOYMENT=<your-deployment-name>
//	export AZURE_OPENAI_API_VERSION=2024-10-21
//
// Run:
//
//	go run ./examples/azure_tools/main.go
//
// If AZURE_OPENAI_API_KEY is not set the example prints the required variables
// and exits cleanly (0) without making any network call.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strings"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
)

// maxToolRounds bounds the tool-call loop so a misbehaving model cannot spin
// forever.
const maxToolRounds = 5

func main() {
	fmt.Println("=== Chronos: Azure OpenAI Tool Calling ===")

	// ────────────────────────────────────────────────────────────────
	// Step 1: Resolve Azure configuration from the environment.
	// Exit gracefully (0) when credentials are absent so `go run` and CI
	// never fail on a missing key.
	// ────────────────────────────────────────────────────────────────
	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	if apiKey == "" {
		printEnvHelp()
		return
	}

	provider := model.NewAzureOpenAIWithConfig(model.AzureConfig{
		ProviderConfig: model.ProviderConfig{
			APIKey:  apiKey,
			BaseURL: os.Getenv("AZURE_OPENAI_ENDPOINT"),
		},
		Deployment: os.Getenv("AZURE_OPENAI_DEPLOYMENT"),
		APIVersion: os.Getenv("AZURE_OPENAI_API_VERSION"),
	})
	fmt.Printf("\n[1] Provider: %s (deployment: %s)\n", provider.Name(), provider.Model())

	// ────────────────────────────────────────────────────────────────
	// Step 2: Register the tools the model may call.
	// ────────────────────────────────────────────────────────────────
	registry := newToolRegistry()
	fmt.Printf("[2] Registered %d tools: %s\n", len(registry.List()), toolNames(registry))

	// ────────────────────────────────────────────────────────────────
	// Step 3: Run a prompt that requires both tools, driving the loop.
	// ────────────────────────────────────────────────────────────────
	ctx := context.Background()
	prompt := "What is the population of France multiplied by 3? " +
		"Use the lookup tool for the population and the calculator for the math."
	fmt.Printf("\n[3] User: %s\n", prompt)

	answer, err := runToolLoop(ctx, provider, registry, prompt)
	if err != nil {
		log.Fatalf("tool loop: %v", err)
	}

	fmt.Printf("\n[4] Final answer:\n%s\n", answer)
}

// runToolLoop sends the prompt with tool definitions and repeatedly executes
// any tools the model requests, feeding the results back, until the model
// returns a final text answer (or maxToolRounds is reached).
func runToolLoop(ctx context.Context, provider model.Provider, registry *tool.Registry, prompt string) (string, error) {
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "You are a precise assistant. Use the provided tools for factual lookups and arithmetic instead of guessing."},
		{Role: model.RoleUser, Content: prompt},
	}

	// Convert the registry into provider-agnostic tool definitions once.
	var tools []model.ToolDefinition
	for _, t := range registry.List() {
		tools = append(tools, model.ToolDefinition{
			Type: "function",
			Function: model.FunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	for round := 1; round <= maxToolRounds; round++ {
		resp, err := provider.Chat(ctx, &model.ChatRequest{
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			return "", fmt.Errorf("chat round %d: %w", round, err)
		}

		if resp.StopReason != model.StopReasonToolCall || len(resp.ToolCalls) == 0 {
			return resp.Content, nil
		}

		// Record the assistant's tool-call turn, then execute each call.
		messages = append(messages, model.Message{
			Role:      model.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			fmt.Printf("    [round %d] tool call: %s(%s)\n", round, tc.Name, tc.Arguments)

			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Arguments), &args)

			var content string
			result, execErr := registry.Execute(ctx, tc.Name, args)
			if execErr != nil {
				content = fmt.Sprintf("Error: %s", execErr.Error())
			} else {
				resultJSON, _ := json.Marshal(result)
				content = string(resultJSON)
			}
			fmt.Printf("    [round %d] tool result: %s\n", round, content)

			messages = append(messages, model.Message{
				Role:       model.RoleTool,
				Content:    content,
				ToolCallID: tc.ID,
				Name:       tc.Name,
			})
		}
	}

	return "", fmt.Errorf("exceeded %d tool-call rounds without a final answer", maxToolRounds)
}

// newToolRegistry builds a registry with a calculator and a lookup tool.
func newToolRegistry() *tool.Registry {
	registry := tool.NewRegistry()

	registry.Register(&tool.Definition{
		Name:        "calculator",
		Description: "Evaluate a basic arithmetic operation on two numbers.",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a":  map[string]any{"type": "number", "description": "First operand"},
				"b":  map[string]any{"type": "number", "description": "Second operand"},
				"op": map[string]any{"type": "string", "enum": []string{"add", "sub", "mul", "div"}, "description": "Operation"},
			},
			"required": []string{"a", "b", "op"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			op, _ := args["op"].(string)
			return calculate(a, b, op)
		},
	})

	registry.Register(&tool.Definition{
		Name:        "lookup",
		Description: "Look up a factual value such as the population of a country.",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entity": map[string]any{"type": "string", "description": "The entity to look up, e.g. 'France population'"},
			},
			"required": []string{"entity"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			entity, _ := args["entity"].(string)
			return lookup(entity), nil
		},
	})

	return registry
}

// calculate performs a single arithmetic operation. Exported behavior is unit
// tested offline (see main_test.go).
func calculate(a, b float64, op string) (map[string]any, error) {
	var result float64
	switch op {
	case "add":
		result = a + b
	case "sub":
		result = a - b
	case "mul":
		result = a * b
	case "div":
		if b == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		result = a / b
	default:
		return nil, fmt.Errorf("unknown operation: %q", op)
	}
	if math.IsInf(result, 0) || math.IsNaN(result) {
		return nil, fmt.Errorf("non-finite result")
	}
	return map[string]any{"result": result}, nil
}

// lookup returns a canned fact for known entities. This is deterministic and
// offline so the example (and its test) never depends on external data.
func lookup(entity string) map[string]any {
	facts := map[string]float64{
		"france population":  68000000,
		"germany population": 84000000,
		"japan population":   125000000,
	}
	key := strings.ToLower(strings.TrimSpace(entity))
	for name, value := range facts {
		if strings.Contains(key, strings.TrimSuffix(name, " population")) {
			return map[string]any{"found": true, "entity": name, "value": value}
		}
	}
	return map[string]any{"found": false, "entity": entity}
}

func toolNames(registry *tool.Registry) string {
	var names []string
	for _, t := range registry.List() {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

func printEnvHelp() {
	fmt.Println("\nAZURE_OPENAI_API_KEY is not set — skipping the live Azure call.")
	fmt.Println("Set these environment variables to run against your resource:")
	fmt.Println("  export AZURE_OPENAI_API_KEY=<your-azure-api-key>")
	fmt.Println("  export AZURE_OPENAI_ENDPOINT=https://<your-resource>.openai.azure.com")
	fmt.Println("  export AZURE_OPENAI_DEPLOYMENT=<your-deployment-name>")
	fmt.Println("  export AZURE_OPENAI_API_VERSION=2024-10-21")
}
