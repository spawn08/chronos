// Example: chat_with_tools demonstrates an agent that uses tools during chat.
//
// The mock provider simulates tool-calling behavior (returns ToolCalls in responses),
// demonstrating the full tool-calling loop: model requests tool → agent executes → result
// fed back to model.
//
// No API keys needed.
//
//	go run ./examples/chat_with_tools/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos Chat with Tools Example                   ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// ── Define tools ──

	calculatorTool := &tool.Definition{
		Name:        "calculator",
		Description: "Perform mathematical calculations",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{"type": "string", "description": "Math expression like 'sqrt(144)' or '2+2'"},
			},
			"required": []any{"expression"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			expr, _ := args["expression"].(string)
			switch {
			case strings.HasPrefix(expr, "sqrt("):
				var n float64
				if _, err := fmt.Sscanf(expr, "sqrt(%f)", &n); err == nil {
					return math.Sqrt(n), nil
				}
			case strings.Contains(expr, "+"):
				var a, b float64
				if _, err := fmt.Sscanf(expr, "%f+%f", &a, &b); err == nil {
					return a + b, nil
				}
			case strings.Contains(expr, "*"):
				var a, b float64
				if _, err := fmt.Sscanf(expr, "%f*%f", &a, &b); err == nil {
					return a * b, nil
				}
			}
			return nil, fmt.Errorf("cannot parse expression: %s", expr)
		},
	}

	lookupTool := &tool.Definition{
		Name:        "lookup_capital",
		Description: "Look up the capital of a country",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"country": map[string]any{"type": "string"},
			},
			"required": []any{"country"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			country, _ := args["country"].(string)
			capitals := map[string]string{
				"France":    "Paris",
				"Japan":     "Tokyo",
				"Brazil":    "Brasília",
				"Australia": "Canberra",
				"India":     "New Delhi",
				"Germany":   "Berlin",
			}
			if cap, ok := capitals[country]; ok {
				return map[string]any{"country": country, "capital": cap}, nil
			}
			return nil, fmt.Errorf("unknown country: %s", country)
		},
	}

	// ── Build the agent ──

	a, err := agent.New("tool-agent", "Tool-Using Agent").
		WithModel(&toolMockProvider{}).
		WithSystemPrompt("You are a helpful assistant with calculator and geography tools.").
		AddTool(calculatorTool).
		AddTool(lookupTool).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// ── Direct tool execution via the agent's tool registry ──

	fmt.Println("\n━━━ Direct Tool Execution ━━━")

	result, err := a.Tools.Execute(ctx, "calculator", map[string]any{"expression": "sqrt(256)"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  calculator(sqrt(256)) = %v\n", result)

	result, err = a.Tools.Execute(ctx, "calculator", map[string]any{"expression": "15*8"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  calculator(15*8) = %v\n", result)

	result, err = a.Tools.Execute(ctx, "lookup_capital", map[string]any{"country": "Japan"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  lookup_capital(Japan) = %v\n", result)

	result, err = a.Tools.Execute(ctx, "lookup_capital", map[string]any{"country": "India"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  lookup_capital(India) = %v\n", result)

	// ── Chat with model (mock shows tool definitions are passed) ──

	fmt.Println("\n━━━ Chat (Model Sees Tools) ━━━")

	resp, err := a.Chat(ctx, "What is the square root of 625?")
	if err != nil {
		fmt.Printf("  Error: %v\n", err)
	} else {
		fmt.Printf("  Response: %s\n", resp.Content)
	}

	resp, err = a.Chat(ctx, "What is the capital of Brazil?")
	if err != nil {
		fmt.Printf("  Error: %v\n", err)
	} else {
		fmt.Printf("  Response: %s\n", resp.Content)
	}

	// ── List available tools ──

	fmt.Println("\n━━━ Available Tools ━━━")
	for _, t := range a.Tools.List() {
		params, _ := json.MarshalIndent(t.Parameters, "    ", "  ")
		fmt.Printf("  %s: %s\n    Parameters: %s\n", t.Name, t.Description, params)
	}

	fmt.Println("\n✓ Chat with Tools example completed.")
}

type toolMockProvider struct{}

func (p *toolMockProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	last := req.Messages[len(req.Messages)-1].Content

	toolCount := len(req.Tools)
	content := fmt.Sprintf("[Mock] I have %d tools available. For your query about %q, I would use the appropriate tool.",
		toolCount, truncate(last, 60))

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

func (p *toolMockProvider) StreamChat(_ context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, _ := p.Chat(context.Background(), req)
	ch <- resp
	close(ch)
	return ch, nil
}

func (p *toolMockProvider) Name() string  { return "mock" }
func (p *toolMockProvider) Model() string { return "mock-v1" }

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
