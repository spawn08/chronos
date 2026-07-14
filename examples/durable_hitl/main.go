// Example: durable_hitl demonstrates a durable, human-in-the-loop (HITL) StateGraph.
//
// The graph pauses at an interrupt node ("approval") and checkpoints its full
// state to SQLite. A separate Runner — simulating a process restart — then
// resumes from that checkpoint and drives the workflow to completion.
//
// No API keys and no network access are required: every node is a pure Go
// function, so this example runs deterministically.
//
//	go run ./examples/durable_hitl/
//
// Durability note
// ---------------
// In Chronos an *interrupt node* checkpoints and pauses BEFORE its function
// runs — it is the gate at which a human must approve. When you resume against
// a graph in which that same node is still an interrupt, the runner re-pauses
// at the gate (the approval has not been granted yet). To advance PAST the gate
// we resume against an "approved" variant of the graph in which the gate node
// is an ordinary node that records the human's decision and lets execution flow
// onward. This mirrors real HITL systems: the paused checkpoint is the request
// for approval; resuming with the approval granted continues the work.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║   Chronos Durable Human-in-the-Loop (HITL) Example     ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// ── 1. Durable storage on disk ──
	// A temp file (rather than ":memory:") lets us prove durability: we close
	// the first store entirely and open a fresh one before resuming, exactly as
	// a restarted process would.
	dir, err := os.MkdirTemp("", "chronos-hitl-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	dbPath := filepath.Join(dir, "hitl.db")

	sessionID := "sess_expense_approval"

	// ── 2. Run the workflow until it pauses at the approval gate ──
	runToPause(ctx, dbPath, sessionID)

	fmt.Println("\n--- (a human reviews the pending request out-of-band) ---")

	// ── 3. Resume from the durable checkpoint and finish the workflow ──
	resumeToCompletion(ctx, dbPath, sessionID)
}

// runToPause builds the "pending" graph (approval is an interrupt node),
// starts a run, and stops when the runner pauses at the gate.
func runToPause(ctx context.Context, dbPath, sessionID string) {
	store, err := sqlite.New(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	// Record a session so it shows up in `chronos sessions` and the dashboard.
	_ = store.CreateSession(ctx, &storage.Session{
		ID:        sessionID,
		AgentID:   "expense-approver",
		Status:    "running",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	pending := pendingGraph()
	runner := graph.NewRunner(pending, store)

	initial := graph.State{"employee": "Ada", "amount": 4200.0, "reason": "conference travel"}
	fmt.Printf("\n[run] starting workflow for %v (amount $%.0f)\n", initial["employee"], initial["amount"])

	rs, err := runner.Run(ctx, sessionID, initial)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("[run] status=%s current_node=%q seq=%d\n", rs.Status, rs.CurrentNode, rs.SeqNum)
	if rs.Status != graph.RunStatusPaused {
		log.Fatalf("expected the run to pause at the approval gate, got %q", rs.Status)
	}
	fmt.Printf("[run] paused — a durable checkpoint is now persisted at %q\n", dbPath)
}

// resumeToCompletion opens a brand-new store (as a restarted process would),
// then resumes against the "approved" graph variant so execution advances past
// the gate and completes.
func resumeToCompletion(ctx context.Context, dbPath, sessionID string) {
	store, err := sqlite.New(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Sanity check: the checkpoint survived the "restart".
	cp, err := store.GetLatestCheckpoint(ctx, sessionID)
	if err != nil {
		log.Fatalf("no durable checkpoint found: %v", err)
	}
	fmt.Printf("[resume] loaded checkpoint node=%q seq=%d state=%v\n", cp.NodeID, cp.SeqNum, cp.State)

	approved := approvedGraph()
	runner := graph.NewRunner(approved, store)

	rs, err := runner.Resume(ctx, sessionID)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("[resume] status=%s\n", rs.Status)
	if rs.Status != graph.RunStatusCompleted {
		log.Fatalf("expected completion after approval, got %q", rs.Status)
	}
	fmt.Printf("[resume] final state: %v\n", rs.State)
	fmt.Println("\n✓ Durable HITL workflow completed after human approval.")
}

// pendingGraph models the workflow BEFORE approval: prepare -> approval(gate) -> disburse.
// The "approval" node is an interrupt node, so the runner checkpoints and pauses
// at the gate without running it.
func pendingGraph() *graph.CompiledGraph {
	g := graph.New("expense-approval").
		AddNode("prepare", prepareNode).
		AddInterruptNode("approval", approvalNode). // pauses here for a human
		AddNode("disburse", disburseNode).
		SetEntryPoint("prepare").
		AddEdge("prepare", "approval").
		AddEdge("approval", "disburse").
		SetFinishPoint("disburse")
	compiled, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	return compiled
}

// approvedGraph is the same workflow AFTER a human granted approval: the gate
// node is now an ordinary node that records the decision and lets flow continue.
func approvedGraph() *graph.CompiledGraph {
	g := graph.New("expense-approval").
		AddNode("prepare", prepareNode).
		AddNode("approval", approvalNode). // no longer an interrupt: approval granted
		AddNode("disburse", disburseNode).
		SetEntryPoint("prepare").
		AddEdge("prepare", "approval").
		AddEdge("approval", "disburse").
		SetFinishPoint("disburse")
	compiled, err := g.Compile()
	if err != nil {
		log.Fatal(err)
	}
	return compiled
}

func prepareNode(_ context.Context, s graph.State) (graph.State, error) {
	fmt.Printf("  [node:prepare] building approval request for %v\n", s["employee"])
	s["request_id"] = "REQ-2026-0714"
	s["status"] = "pending_approval"
	return s, nil
}

// approvalNode runs only after a human has approved (i.e. only on the approved
// graph). It records who approved and unblocks disbursement.
func approvalNode(_ context.Context, s graph.State) (graph.State, error) {
	fmt.Println("  [node:approval] approval granted — recording decision")
	s["approved"] = true
	s["approver"] = "manager@corp.example"
	s["status"] = "approved"
	return s, nil
}

func disburseNode(_ context.Context, s graph.State) (graph.State, error) {
	fmt.Printf("  [node:disburse] disbursing $%.0f to %v (approved by %v)\n",
		s["amount"], s["employee"], s["approver"])
	s["status"] = "disbursed"
	return s, nil
}
