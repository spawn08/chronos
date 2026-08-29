// Example: yaml_dashboard demonstrates a durable graph declared entirely in
// YAML (WC-C-004) — the YAML equivalent of examples/dashboard's hand-built Go
// graph. agent.yaml declares a `graph:` block (three "passthrough" nodes,
// with "gate" as a human-in-the-loop interrupt) and sets `durable: true`, so
// its compiled graph is registered with ChronosOS exactly as if it had been
// built by hand in Go and passed to chronosos.WithGraphs.
//
// This program is the SDK-level equivalent of running:
//
//	chronos -c examples/yaml_dashboard/agent.yaml serve :8420
//
// which loads the same YAML, compiles the same graph, and wires it into the
// dashboard automatically — no application code required. This example
// spells that out in Go so you can see exactly what `chronos serve` does
// under the hood.
//
// No API key is required: the model provider is configured but never
// called — every graph node here is a "passthrough" node, not a "model"
// node. (See website/docs/guides/yaml-dashboard.md for "model"/"tool"/
// "subagent" node examples.)
//
//	go run ./examples/yaml_dashboard/
package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"runtime"

	chronosos "github.com/spawn08/chronos/os"
	"github.com/spawn08/chronos/os/dashboard"

	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║   Chronos YAML Graph → ChronosOS Dashboard Example     ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	ctx := context.Background()

	// Resolve agent.yaml next to this source file, so the example runs the
	// same way regardless of the caller's working directory.
	_, thisFile, _, _ := runtime.Caller(0)
	cfgPath := filepath.Join(filepath.Dir(thisFile), "agent.yaml")

	fc, err := agent.LoadFile(cfgPath)
	if err != nil {
		log.Fatal(err)
	}

	agents, err := agent.BuildAll(ctx, fc)
	if err != nil {
		log.Fatal(err)
	}

	// agent.DurableGraphs is exactly what buildServeGraphOptions (cli/cmd/
	// serve.go) uses to register every durable agent's already-compiled graph
	// with the dashboard for `chronos serve`.
	registry := dashboard.GraphRegistry(agent.DurableGraphs(fc, agents))

	// Unlike `chronos serve` (which always uses its own CHRONOS_STORAGE_*
	// main store, independent of any agent's `storage:` block — see that
	// function's doc comment), this example deliberately reuses the
	// expense-approver agent's own storage.Storage AS the ChronosOS server's
	// main store. That is a valid, intentional pattern when you embed the SDK
	// yourself and want exactly one store shared by both the agent and the
	// control plane.
	expenseApprover, ok := agents["expense-approver"]
	if !ok {
		log.Fatal(`agent.yaml must define an agent with id "expense-approver"`)
	}

	s := chronosos.NewWithOptions(":8420", expenseApprover.Storage,
		chronosos.WithGraphs(registry),
	)

	fmt.Println("\n✓ Dashboard running. Start a run, then open http://localhost:8420/dashboard/:")
	fmt.Print(`
    curl -X POST http://localhost:8420/api/dashboard/runs \
      -H 'Content-Type: application/json' \
      -d '{"agent_id":"expense-approver"}'
`)
	fmt.Println("    1. the response's \"session_id\" is the id to select in the session list")
	fmt.Println("    2. the graph renders with 'gate' marked as an interrupt node")
	fmt.Println("    3. click Resume to advance the paused run past the gate")
	fmt.Println("    4. use the checkpoint list to time-travel back to an earlier step")
	fmt.Println("\n(Ctrl+C to stop)")

	if err := s.Start(ctx); err != nil {
		log.Fatal(err)
	}
}
