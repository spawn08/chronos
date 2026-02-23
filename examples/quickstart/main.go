// Chronos Quickstart — a minimal agent with SQLite storage and a durable StateGraph.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/chronos-ai/chronos/engine/graph"
	"github.com/chronos-ai/chronos/sdk/agent"
	"github.com/chronos-ai/chronos/storage/adapters/sqlite"
)

func main() {
	ctx := context.Background()

	// 1. Open SQLite storage
	store, err := sqlite.New("quickstart.db")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	// 2. Define a simple graph: greet -> classify -> respond
	g := graph.New("quickstart").
		AddNode("greet", func(_ context.Context, s graph.State) (graph.State, error) {
			s["greeting"] = fmt.Sprintf("Hello, %s!", s["user"])
			return s, nil
		}).
		AddNode("classify", func(_ context.Context, s graph.State) (graph.State, error) {
			s["intent"] = "general_question"
			return s, nil
		}).
		AddNode("respond", func(_ context.Context, s graph.State) (graph.State, error) {
			s["response"] = fmt.Sprintf("I classified your intent as %q. How can I help?", s["intent"])
			return s, nil
		}).
		SetEntryPoint("greet").
		AddEdge("greet", "classify").
		AddEdge("classify", "respond").
		SetFinishPoint("respond")

	// 3. Build the agent
	a, err := agent.New("quickstart-agent", "Quickstart Agent").
		WithStorage(store).
		WithGraph(g).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// 4. Run it
	result, err := a.Run(ctx, map[string]any{"user": "World"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Result: %v\n", result.State)
}
