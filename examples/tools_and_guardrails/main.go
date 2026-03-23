// Example: tools_and_guardrails demonstrates the tool registry with permission levels
// and guardrail engine for input/output validation.
//
// This example runs entirely locally with a mock provider — no API keys needed.
//
//	go run ./examples/tools_and_guardrails/
package main

import (
	"context"
	"fmt"
	"log"
	"math"

	"github.com/spawn08/chronos/engine/guardrails"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos Tools & Guardrails Example                ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// ── 1. Define tools with different permission levels ──

	calcTool := &tool.Definition{
		Name:        "calculate",
		Description: "Perform basic arithmetic",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{"type": "string", "enum": []string{"add", "subtract", "multiply", "divide", "sqrt"}},
				"a":         map[string]any{"type": "number"},
				"b":         map[string]any{"type": "number"},
			},
			"required": []any{"operation", "a"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			op, _ := args["operation"].(string)
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			switch op {
			case "add":
				return a + b, nil
			case "subtract":
				return a - b, nil
			case "multiply":
				return a * b, nil
			case "divide":
				if b == 0 {
					return nil, fmt.Errorf("division by zero")
				}
				return a / b, nil
			case "sqrt":
				return math.Sqrt(a), nil
			default:
				return nil, fmt.Errorf("unknown operation: %s", op)
			}
		},
	}

	weatherTool := &tool.Definition{
		Name:        "get_weather",
		Description: "Get current weather for a city",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{"type": "string"},
			},
			"required": []any{"city"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			city, _ := args["city"].(string)
			return map[string]any{
				"city":        city,
				"temperature": 22,
				"condition":   "sunny",
				"humidity":    45,
			}, nil
		},
	}

	dangerousTool := &tool.Definition{
		Name:        "delete_database",
		Description: "Delete the entire database (dangerous!)",
		Permission:  tool.PermDeny,
		Handler: func(_ context.Context, _ map[string]any) (any, error) {
			return nil, fmt.Errorf("this should never execute")
		},
	}

	approvalTool := &tool.Definition{
		Name:        "send_email",
		Description: "Send an email (requires approval)",
		Permission:  tool.PermRequireApproval,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"to":      map[string]any{"type": "string"},
				"subject": map[string]any{"type": "string"},
			},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			to, _ := args["to"].(string)
			subject, _ := args["subject"].(string)
			return fmt.Sprintf("Email sent to %s: %s", to, subject), nil
		},
	}

	// ── 2. Build the agent with tools and guardrails ──

	a, err := agent.New("tools-demo", "Tools Demo Agent").
		WithModel(&mockProvider{}).
		WithSystemPrompt("You are a helpful assistant with calculator and weather tools.").
		AddTool(calcTool).
		AddTool(weatherTool).
		AddTool(dangerousTool).
		AddTool(approvalTool).
		AddInputGuardrail("profanity-filter", &guardrails.BlocklistGuardrail{
			Blocklist: []string{"hack", "exploit", "attack"},
		}).
		AddOutputGuardrail("length-limit", &guardrails.MaxLengthGuardrail{
			MaxChars: 5000,
		}).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// Set approval handler for tools requiring human approval
	a.Tools.SetApprovalHandler(func(_ context.Context, toolName string, args map[string]any) (bool, error) {
		fmt.Printf("  [APPROVAL] Tool %q requested with args %v — auto-approving for demo\n", toolName, args)
		return true, nil
	})

	// ── 3. Demonstrate tool execution directly ──

	fmt.Println("\n━━━ Tool Execution (Direct) ━━━")

	result, err := a.Tools.Execute(ctx, "calculate", map[string]any{
		"operation": "multiply",
		"a":         float64(7),
		"b":         float64(6),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  calculate(7 * 6) = %v\n", result)

	result, err = a.Tools.Execute(ctx, "calculate", map[string]any{
		"operation": "sqrt",
		"a":         float64(144),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  calculate(sqrt(144)) = %v\n", result)

	result, err = a.Tools.Execute(ctx, "get_weather", map[string]any{
		"city": "Tokyo",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  get_weather(Tokyo) = %v\n", result)

	// ── 4. Demonstrate denied tool ──

	fmt.Println("\n━━━ Permission: Denied Tool ━━━")
	_, err = a.Tools.Execute(ctx, "delete_database", map[string]any{})
	fmt.Printf("  delete_database → %v (expected: denied)\n", err)

	// ── 5. Demonstrate approval-required tool ──

	fmt.Println("\n━━━ Permission: Approval-Required Tool ━━━")
	result, err = a.Tools.Execute(ctx, "send_email", map[string]any{
		"to":      "user@example.com",
		"subject": "Meeting tomorrow",
	})
	if err != nil {
		fmt.Printf("  send_email → error: %v\n", err)
	} else {
		fmt.Printf("  send_email → %v\n", result)
	}

	// ── 6. Demonstrate guardrails ──

	fmt.Println("\n━━━ Input Guardrails ━━━")

	_, err = a.Chat(ctx, "How do I hack into a system?")
	fmt.Printf("  Blocked input: %v\n", err)

	resp, err := a.Chat(ctx, "What is the weather like today?")
	if err != nil {
		fmt.Printf("  Allowed input error: %v\n", err)
	} else {
		fmt.Printf("  Allowed input response: %.80s\n", resp.Content)
	}

	// ── 7. List registered tools ──

	fmt.Println("\n━━━ Registered Tools ━━━")
	for _, t := range a.Tools.List() {
		fmt.Printf("  %-20s  permission: %-20s  %s\n", t.Name, t.Permission, t.Description)
	}

	fmt.Println("\n✓ Tools & Guardrails example completed.")
}

type mockProvider struct{}

func (m *mockProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	last := req.Messages[len(req.Messages)-1].Content
	return &model.ChatResponse{
		Content:    fmt.Sprintf("[Mock] Response to: %.80s", last),
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
