// Example: a2a_interop shows Chronos interoperating over the Agent-to-Agent
// (A2A) protocol (WC-B-001). It stands up a *durable* A2A server — tasks are
// backed by the engine/queue and survive restarts — publishes an agent card from
// a skill registry, then acts as a client: it discovers the card, delegates a
// task, and streams the result to completion. It also shows the same remote agent
// wrapped as a Chronos tool (a delegated subagent). No LLM or API key required.
//
//	go run ./examples/a2a_interop/
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spawn08/chronos/engine/queue"
	"github.com/spawn08/chronos/sdk/protocol/a2a"
	"github.com/spawn08/chronos/sdk/skill"
	"github.com/spawn08/chronos/storage/adapters/sqlite"

	"net/http/httptest"

	_ "modernc.org/sqlite"
)

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║          Chronos A2A Interop Example (durable)         ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// --- Durable backing: a SQLite-backed checkpoint store + work queue. -------
	dir, err := os.MkdirTemp("", "a2a-example")
	must(err)
	defer os.RemoveAll(dir)

	store, err := sqlite.New(filepath.Join(dir, "store.db"), sqlite.WithMaxOpenConns(4))
	must(err)
	defer store.Close()
	must(store.Migrate(ctx))

	qdb, err := sql.Open("sqlite", filepath.Join(dir, "queue.db")+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	must(err)
	defer qdb.Close()
	q := queue.New(queue.NewSQLStore(qdb, queue.DialectSQLite), queue.Config{})
	must(q.Migrate(ctx))

	// The agent's work: an "uppercase" task with a small delay so the client sees
	// the task transition pending → running → completed over the stream.
	handler := func(_ context.Context, task *a2a.Task) error {
		time.Sleep(150 * time.Millisecond)
		task.Output = strings.ToUpper(task.Input)
		return nil
	}
	ds := a2a.NewDurableStore(q, store, handler)

	// Drive execution with a worker (+ reaper for orphan recovery on restart).
	worker, err := queue.NewWorker(q, ds.Executor, queue.WorkerConfig{ID: "a2a-worker-1"})
	must(err)
	go func() { _ = worker.Run(ctx) }()
	go func() { _ = queue.NewReaper(q, time.Second).Run(ctx) }()

	// --- The A2A server, advertising capabilities from a skill registry. -------
	skills := skill.NewRegistry()
	skills.Register(&skill.Skill{Name: "uppercase", Version: "1.0", Description: "Upper-cases text"})
	card := a2a.CardFromSkills("chronos-agent", "A durable Chronos A2A agent", "1.0", skills)

	srv := httptest.NewServer(a2a.NewServerWithStore(card, ds))
	defer srv.Close()

	// --- Client side: discover, delegate, stream. ------------------------------
	client := a2a.NewClient(srv.URL)

	got, err := client.GetAgentCard(ctx)
	must(err)
	fmt.Printf("\n• Discovered agent %q (v%s): capabilities=%v\n", got.Name, got.Version, got.Capabilities)

	task, err := client.CreateTask(ctx, "hello from a remote agent", nil)
	must(err)
	fmt.Printf("• Delegated task %s (status=%s)\n", task.ID, task.Status)

	fmt.Println("• Streaming task updates:")
	tasks, errs := client.StreamTask(ctx, task.ID)
	for snap := range tasks {
		fmt.Printf("    → status=%-9s output=%q\n", snap.Status, snap.Output)
	}
	if err := <-errs; err != nil {
		fmt.Printf("    stream error: %v\n", err)
	}

	// --- The same remote agent as a Chronos tool (delegated subagent). ---------
	toolDef := a2a.NewRemoteAgentTool("remote_uppercase", "Delegate text to the remote agent", client)
	result, err := toolDef.Handler(ctx, map[string]any{"task": "delegated via a tool"})
	must(err)
	fmt.Printf("\n• Tool delegation result: %v\n", result)

	fmt.Println("\n✓ A2A round-trip complete.")
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
