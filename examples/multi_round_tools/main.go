// Example: multi_round_tools demonstrates an agent performing MULTIPLE sequential
// tool-calling rounds, where the result of tool A feeds the arguments of tool B.
//
// A small deterministic mock model.Provider drives the loop, so the example runs
// with NO API keys and NO network access. The mock decides its next action purely
// from the most recent message:
//
//	round 0: user asks a question            -> call resolve_customer(name)
//	round 1: resolve_customer returned an id -> call fetch_orders(customer_id)   (A feeds B)
//	round 2: fetch_orders returned orders     -> emit the final natural-language answer
//
// Chronos' agent loop (Agent.Chat) runs the tools, feeds each result back to the
// model, and repeats until the model stops requesting tools.
//
//	go run ./examples/multi_round_tools/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║     Chronos Multi-Round Tool-Calling Example           ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// ── Tool A: resolve a customer name to an internal record ──
	resolveCustomer := &tool.Definition{
		Name:        "resolve_customer",
		Description: "Resolve a customer's display name to their internal customer record",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Customer display name"},
			},
			"required": []any{"name"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			name, _ := args["name"].(string)
			fmt.Printf("  [tool:resolve_customer] name=%q\n", name)
			// A tiny deterministic directory.
			directory := map[string]string{
				"Ada Lovelace": "C-1007",
				"Alan Turing":  "C-1042",
				"Grace Hopper": "C-1099",
			}
			id, ok := directory[name]
			if !ok {
				return nil, fmt.Errorf("no customer named %q", name)
			}
			return map[string]any{"customer_id": id, "tier": "gold"}, nil
		},
	}

	// ── Tool B: fetch orders for a resolved customer_id ──
	fetchOrders := &tool.Definition{
		Name:        "fetch_orders",
		Description: "Fetch the recent orders for a customer by internal customer_id",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"customer_id": map[string]any{"type": "string", "description": "Internal customer id, e.g. C-1007"},
			},
			"required": []any{"customer_id"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			customerID, _ := args["customer_id"].(string)
			fmt.Printf("  [tool:fetch_orders] customer_id=%q\n", customerID)
			ordersByCustomer := map[string]int{"C-1007": 3, "C-1042": 1, "C-1099": 7}
			count := ordersByCustomer[customerID]
			return map[string]any{"customer_id": customerID, "order_count": count}, nil
		},
	}

	// ── Build the agent with the deterministic mock provider ──
	a, err := agent.New("orders-agent", "Orders Assistant").
		WithModel(&sequentialToolMock{}).
		WithSystemPrompt("You help staff look up customer order history.").
		AddTool(resolveCustomer).
		AddTool(fetchOrders).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	question := "How many orders does Ada Lovelace have?"
	fmt.Printf("\nUser: %s\n\n", question)

	resp, err := a.Chat(ctx, question)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\nAssistant: %s\n", resp.Content)
	fmt.Println("\n✓ Completed two sequential tool rounds (resolve_customer → fetch_orders).")
}

// sequentialToolMock is a deterministic model.Provider that sequences two tool
// rounds. It never makes a network call; it chooses its next step from the most
// recent message, threading tool A's output into tool B's arguments.
type sequentialToolMock struct{}

func (m *sequentialToolMock) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	last := req.Messages[len(req.Messages)-1]

	// If the most recent message is a tool result, decide the next step from it.
	if last.Role == model.RoleTool {
		switch last.Name {
		case "resolve_customer":
			// Round 1: read tool A's output and feed customer_id into tool B.
			var out struct {
				CustomerID string `json:"customer_id"`
			}
			_ = json.Unmarshal([]byte(last.Content), &out)
			return toolCallResponse("call_2", "fetch_orders",
				map[string]any{"customer_id": out.CustomerID}), nil

		case "fetch_orders":
			// Round 2: read tool B's output and produce the final answer.
			var out struct {
				CustomerID string `json:"customer_id"`
				OrderCount int    `json:"order_count"`
			}
			_ = json.Unmarshal([]byte(last.Content), &out)
			content := fmt.Sprintf("Customer %s has %d recent orders.", out.CustomerID, out.OrderCount)
			return &model.ChatResponse{Role: model.RoleAssistant, Content: content, StopReason: model.StopReasonEnd}, nil
		}
	}

	// Round 0: the user asked a question — start by resolving the customer.
	return toolCallResponse("call_1", "resolve_customer",
		map[string]any{"name": "Ada Lovelace"}), nil
}

func (m *sequentialToolMock) StreamChat(ctx context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, err := m.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *sequentialToolMock) Name() string  { return "sequential-tool-mock" }
func (m *sequentialToolMock) Model() string { return "mock-v1" }

// toolCallResponse builds a ChatResponse that asks the agent to invoke a tool.
func toolCallResponse(id, name string, args map[string]any) *model.ChatResponse {
	raw, _ := json.Marshal(args)
	return &model.ChatResponse{
		Role:       model.RoleAssistant,
		StopReason: model.StopReasonToolCall,
		ToolCalls: []model.ToolCall{{
			ID:        id,
			Name:      name,
			Arguments: string(raw),
		}},
	}
}
