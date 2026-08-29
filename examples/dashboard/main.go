// Example: dashboard demonstrates the Chronos visual studio / graph debugger
// (WC-C-002). It runs a small expense-approval workflow to its human-in-the-
// loop gate (so there is something to inspect), then starts ChronosOS with
// the dashboard wired to that workflow's graph.
//
// No API keys or network access are required: every node is a pure Go
// function and storage is in-memory, so the workflow itself runs
// deterministically. Open the printed URL in a browser to watch the session
// and its paused status, see the graph render with "gate" marked as an
// interrupt, inspect the checkpoint history and time-travel to an earlier
// one, and click Resume to advance the paused run past the gate.
//
//	go run ./examples/dashboard/
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	chronosos "github.com/spawn08/chronos/os"
	"github.com/spawn08/chronos/os/dashboard"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

const (
	agentID   = "expense-approver"
	sessionID = "sess_expense_approval"
)

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║        Chronos Dashboard / Graph Debugger Example      ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	ctx := context.Background()
	store := memory.New()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	if err := store.CreateSession(ctx, &storage.Session{
		ID:        sessionID,
		AgentID:   agentID,
		Status:    "running",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		log.Fatal(err)
	}

	compiled := expenseGraph()
	runner := graph.NewRunner(compiled, store)

	initial := graph.State{"employee": "Ada", "amount": 4200.0, "reason": "conference travel"}
	rs, err := runner.Run(ctx, sessionID, initial)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n[run] paused at %q awaiting approval (seq=%d)\n", rs.CurrentNode, rs.SeqNum)

	s := chronosos.NewWithOptions(":8420", store,
		chronosos.WithGraphs(dashboard.GraphRegistry{agentID: compiled}),
	)

	fmt.Println("\n✓ Dashboard running. Open http://localhost:8420/dashboard/ in a browser:")
	fmt.Println("    1. select 'sess_expense_approval' in the session list")
	fmt.Println("    2. the graph renders with 'gate' marked as an interrupt node")
	fmt.Println("    3. click Resume to advance the paused run past the gate")
	fmt.Println("    4. use the checkpoint list to time-travel back to an earlier step")
	fmt.Println("\n(Ctrl+C to stop)")

	if err := s.Start(ctx); err != nil {
		log.Fatal(err)
	}
}

// expenseGraph models prepare -> gate(interrupt) -> disburse, matching
// examples/durable_hitl's shape so it pauses immediately for the dashboard to
// show a real interrupt.
func expenseGraph() *graph.CompiledGraph {
	g := graph.New(agentID).
		AddNode("prepare", prepareNode).
		AddInterruptNode("gate", gateNode).
		AddNode("disburse", disburseNode).
		SetEntryPoint("prepare").
		AddEdge("prepare", "gate").
		AddEdge("gate", "disburse").
		SetFinishPoint("disburse")
	compiled, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	return compiled
}

func prepareNode(_ context.Context, s graph.State) (graph.State, error) {
	s["request_id"] = "REQ-2026-0714"
	s["status"] = "pending_approval"
	return s, nil
}

func gateNode(_ context.Context, s graph.State) (graph.State, error) {
	s["approved"] = true
	s["status"] = "approved"
	return s, nil
}

func disburseNode(_ context.Context, s graph.State) (graph.State, error) {
	s["status"] = "disbursed"
	return s, nil
}
