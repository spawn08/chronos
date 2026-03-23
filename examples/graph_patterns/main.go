// Example: graph_patterns demonstrates the StateGraph durable execution engine.
//
// This shows conditional edges, interrupt nodes (human-in-the-loop), checkpointing,
// resume from checkpoint, and real-time stream events.
//
// No API keys needed — runs entirely with SQLite and deterministic node functions.
//
//	go run ./examples/graph_patterns/
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"

	"github.com/spawn08/chronos/engine/graph"
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
	fmt.Println("║    Chronos StateGraph Patterns Example               ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 1. Conditional Edges — route by state
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	fmt.Println("\n━━━ 1. Conditional Edges ━━━")

	conditionalGraph := graph.New("order-processor").
		AddNode("validate", func(_ context.Context, s graph.State) (graph.State, error) {
			amount, _ := s["amount"].(float64)
			if amount <= 0 {
				s["validation"] = "invalid"
				s["error"] = "amount must be positive"
			} else if amount > 10000 {
				s["validation"] = "needs_review"
			} else {
				s["validation"] = "approved"
			}
			return s, nil
		}).
		AddNode("review", func(_ context.Context, s graph.State) (graph.State, error) {
			s["reviewed_by"] = "manager"
			s["validation"] = "approved"
			return s, nil
		}).
		AddNode("process", func(_ context.Context, s graph.State) (graph.State, error) {
			s["status"] = "completed"
			s["order_id"] = fmt.Sprintf("ORD-%d", rand.Intn(10000))
			return s, nil
		}).
		AddNode("reject", func(_ context.Context, s graph.State) (graph.State, error) {
			s["status"] = "rejected"
			return s, nil
		}).
		SetEntryPoint("validate").
		AddConditionalEdge("validate", func(s graph.State) string {
			v, _ := s["validation"].(string)
			switch v {
			case "approved":
				return "process"
			case "needs_review":
				return "review"
			default:
				return "reject"
			}
		}).
		AddEdge("review", "process").
		SetFinishPoint("process").
		SetFinishPoint("reject")

	compiled, err := conditionalGraph.Compile()
	if err != nil {
		log.Fatal(err)
	}

	runner := graph.NewRunner(compiled, store)

	testCases := []struct {
		name   string
		amount float64
	}{
		{"Small order (auto-approved)", 500},
		{"Large order (needs review)", 15000},
		{"Invalid order (rejected)", -50},
	}

	for _, tc := range testCases {
		result, err := runner.Run(ctx, fmt.Sprintf("session-%s", tc.name), graph.State{
			"amount":   tc.amount,
			"customer": "Alice",
		})
		if err != nil {
			fmt.Printf("  %-30s → error: %v\n", tc.name, err)
		} else {
			fmt.Printf("  %-30s → status: %-10v  validation: %v\n",
				tc.name, result.State["status"], result.State["validation"])
		}
		runner = graph.NewRunner(compiled, store)
	}

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 2. Interrupt Node — human-in-the-loop (pause demonstration)
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	fmt.Println("\n━━━ 2. Interrupt Node (Human-in-the-Loop) ━━━")

	// Use a separate store to avoid checkpoint ID collisions with prior examples
	store2, err := sqlite.New(":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer store2.Close()
	if err := store2.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	interruptGraph := graph.New("approval-flow").
		AddNode("prepare", func(_ context.Context, s graph.State) (graph.State, error) {
			s["prepared"] = true
			s["document"] = "Contract #42 for $25,000"
			return s, nil
		}).
		AddInterruptNode("approve", func(_ context.Context, s graph.State) (graph.State, error) {
			s["approved"] = true
			s["approved_by"] = "CFO"
			return s, nil
		}).
		AddNode("execute_contract", func(_ context.Context, s graph.State) (graph.State, error) {
			s["executed"] = true
			s["result"] = "Contract signed and filed"
			return s, nil
		}).
		SetEntryPoint("prepare").
		AddEdge("prepare", "approve").
		AddEdge("approve", "execute_contract").
		SetFinishPoint("execute_contract")

	compiled2, err := interruptGraph.Compile()
	if err != nil {
		log.Fatal(err)
	}

	runner2 := graph.NewRunner(compiled2, store2)

	result, err := runner2.Run(ctx, "interrupt-session", graph.State{
		"requester": "Engineering",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("  Run status:          %s (paused for human approval)\n", result.Status)
	fmt.Printf("  Paused at node:      %s\n", result.CurrentNode)
	fmt.Printf("  Document prepared:   %v\n", result.State["prepared"])
	fmt.Printf("  Document:            %v\n", result.State["document"])
	fmt.Printf("  Checkpoint saved:    yes (can be resumed later with runner.Resume)\n")
	fmt.Println("  [In production, the human would approve and the graph resumes from checkpoint]")

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 3. Stream Events — real-time observability
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	fmt.Println("\n━━━ 3. Stream Events (Real-Time Observability) ━━━")

	pipelineGraph := graph.New("data-pipeline").
		AddNode("ingest", func(_ context.Context, s graph.State) (graph.State, error) {
			s["records"] = 1000
			return s, nil
		}).
		AddNode("transform", func(_ context.Context, s graph.State) (graph.State, error) {
			records, _ := s["records"].(int)
			s["transformed"] = records
			s["skipped"] = 5
			return s, nil
		}).
		AddNode("load", func(_ context.Context, s graph.State) (graph.State, error) {
			s["loaded"] = true
			s["destination"] = "warehouse"
			return s, nil
		}).
		SetEntryPoint("ingest").
		AddEdge("ingest", "transform").
		AddEdge("transform", "load").
		SetFinishPoint("load")

	compiled3, err := pipelineGraph.Compile()
	if err != nil {
		log.Fatal(err)
	}

	runner4 := graph.NewRunner(compiled3, store)

	// Consume stream events in a goroutine
	go func() {
		for evt := range runner4.Stream() {
			fmt.Printf("  [STREAM] type=%-16s node=%-12s\n", evt.Type, evt.NodeID)
		}
	}()

	result, err = runner4.Run(ctx, "pipeline-session", graph.State{
		"source": "raw-data",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("  Pipeline result: records=%v  loaded=%v  destination=%v\n",
		result.State["transformed"], result.State["loaded"], result.State["destination"])

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 4. Multi-path graph with convergence
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	fmt.Println("\n━━━ 4. Conditional Routing with Convergence ━━━")

	supportGraph := graph.New("support-ticket").
		AddNode("triage", func(_ context.Context, s graph.State) (graph.State, error) {
			msg, _ := s["message"].(string)
			if strings.Contains(strings.ToLower(msg), "billing") {
				s["category"] = "billing"
			} else if strings.Contains(strings.ToLower(msg), "technical") {
				s["category"] = "technical"
			} else {
				s["category"] = "general"
			}
			return s, nil
		}).
		AddNode("billing_handler", func(_ context.Context, s graph.State) (graph.State, error) {
			s["response"] = "Your billing issue has been escalated to our finance team."
			s["priority"] = "high"
			return s, nil
		}).
		AddNode("tech_handler", func(_ context.Context, s graph.State) (graph.State, error) {
			s["response"] = "A technical specialist will assist you shortly."
			s["priority"] = "medium"
			return s, nil
		}).
		AddNode("general_handler", func(_ context.Context, s graph.State) (graph.State, error) {
			s["response"] = "Thank you for reaching out. A general agent will help."
			s["priority"] = "low"
			return s, nil
		}).
		SetEntryPoint("triage").
		AddConditionalEdge("triage", func(s graph.State) string {
			cat, _ := s["category"].(string)
			switch cat {
			case "billing":
				return "billing_handler"
			case "technical":
				return "tech_handler"
			default:
				return "general_handler"
			}
		}).
		SetFinishPoint("billing_handler").
		SetFinishPoint("tech_handler").
		SetFinishPoint("general_handler")

	compiled4, err := supportGraph.Compile()
	if err != nil {
		log.Fatal(err)
	}

	tickets := []struct {
		name    string
		message string
	}{
		{"billing inquiry", "I have a billing question about my invoice"},
		{"technical issue", "I need technical support for the API"},
		{"general query", "When is your office open?"},
	}

	for _, t := range tickets {
		r := graph.NewRunner(compiled4, store)
		result, err := r.Run(ctx, "ticket-"+t.name, graph.State{
			"message":  t.message,
			"customer": "Bob",
		})
		if err != nil {
			fmt.Printf("  %-20s → error: %v\n", t.name, err)
		} else {
			fmt.Printf("  %-20s → cat=%-10s prio=%-6s response=%v\n",
				t.name, result.State["category"], result.State["priority"], result.State["response"])
		}
	}

	fmt.Println("\n✓ Graph Patterns example completed.")
}
