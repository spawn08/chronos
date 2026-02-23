// Example: multi_agent demonstrates agents communicating like human developers.
//
// Three agents work together as a software team:
//   - Architect: decomposes requirements into tasks
//   - Developer: implements the tasks
//   - Reviewer: reviews the implementation
//
// They communicate via the protocol bus, delegating tasks, sharing results,
// and broadcasting status updates — just like a real development team.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/chronos-ai/chronos/engine/graph"
	"github.com/chronos-ai/chronos/sdk/agent"
	"github.com/chronos-ai/chronos/sdk/team"
	"github.com/chronos-ai/chronos/storage/adapters/sqlite"
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

	// --- Agent 1: Architect ---
	architectGraph := graph.New("architect-flow").
		AddNode("analyze", func(_ context.Context, s graph.State) (graph.State, error) {
			requirement := s["requirement"]
			s["architecture"] = fmt.Sprintf("Architecture plan for: %v", requirement)
			s["tasks"] = []string{"design API", "implement handlers", "add tests"}
			s["status"] = "architecture_complete"
			fmt.Println("[Architect] Analyzed requirements and produced architecture plan")
			return s, nil
		}).
		SetEntryPoint("analyze").
		SetFinishPoint("analyze")

	architect, err := agent.New("architect", "Architect").
		Description("Analyzes requirements and produces architecture plans").
		AddCapability("architecture").
		AddCapability("planning").
		WithStorage(store).
		WithGraph(architectGraph).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// --- Agent 2: Developer ---
	developerGraph := graph.New("developer-flow").
		AddNode("implement", func(_ context.Context, s graph.State) (graph.State, error) {
			arch := s["architecture"]
			s["implementation"] = fmt.Sprintf("Code implementing: %v", arch)
			s["tests"] = "unit tests passing"
			s["status"] = "implementation_complete"
			fmt.Println("[Developer] Implemented the architecture plan")
			return s, nil
		}).
		SetEntryPoint("implement").
		SetFinishPoint("implement")

	developer, err := agent.New("developer", "Developer").
		Description("Implements features based on architecture plans").
		AddCapability("coding").
		AddCapability("testing").
		WithStorage(store).
		WithGraph(developerGraph).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// --- Agent 3: Reviewer ---
	reviewerGraph := graph.New("reviewer-flow").
		AddNode("review", func(_ context.Context, s graph.State) (graph.State, error) {
			impl := s["implementation"]
			s["review"] = fmt.Sprintf("Review of: %v — LGTM with minor suggestions", impl)
			s["approved"] = true
			s["status"] = "review_complete"
			fmt.Println("[Reviewer] Reviewed implementation and approved")
			return s, nil
		}).
		SetEntryPoint("review").
		SetFinishPoint("review")

	reviewer, err := agent.New("reviewer", "Reviewer").
		Description("Reviews code implementations for quality and correctness").
		AddCapability("code_review").
		AddCapability("quality_assurance").
		WithStorage(store).
		WithGraph(reviewerGraph).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// --- Assemble the team ---

	// Sequential strategy: architect → developer → reviewer
	fmt.Println("=== Sequential Team (architect → developer → reviewer) ===")
	seqTeam := team.New("dev-team", "Development Team", team.StrategySequential).
		AddAgent(architect).
		AddAgent(developer).
		AddAgent(reviewer)

	result, err := seqTeam.Run(ctx, graph.State{
		"requirement": "Build a REST API for user management",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\nFinal result:")
	fmt.Printf("  Architecture: %v\n", result["architecture"])
	fmt.Printf("  Implementation: %v\n", result["implementation"])
	fmt.Printf("  Review: %v\n", result["review"])
	fmt.Printf("  Approved: %v\n", result["approved"])

	// Show the communication history
	fmt.Printf("\n  Messages exchanged: %d\n", len(seqTeam.MessageHistory()))
	for i, msg := range seqTeam.MessageHistory() {
		fmt.Printf("    [%d] %s → %s: %s (%s)\n", i+1, msg.From, msg.To, msg.Subject, msg.Type)
	}

	// --- Coordinator strategy ---
	fmt.Println("\n=== Coordinator Team (architect leads, delegates to developer & reviewer) ===")

	store2, _ := sqlite.New(":memory:")
	defer store2.Close()
	_ = store2.Migrate(ctx)

	arch2, _ := agent.New("arch2", "Lead Architect").
		Description("Leads the team and decomposes tasks").
		WithStorage(store2).
		WithGraph(architectGraph).
		Build()

	dev2, _ := agent.New("dev2", "Developer").
		Description("Implements features").
		WithStorage(store2).
		WithGraph(developerGraph).
		Build()

	rev2, _ := agent.New("rev2", "Reviewer").
		Description("Reviews code").
		WithStorage(store2).
		WithGraph(reviewerGraph).
		Build()

	coordTeam := team.New("coord-team", "Coordinated Team", team.StrategyCoordinator).
		AddAgent(arch2). // first agent is the coordinator
		AddAgent(dev2).
		AddAgent(rev2)

	result2, err := coordTeam.Run(ctx, graph.State{
		"requirement": "Add authentication to the API",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\nFinal result:")
	fmt.Printf("  Approved: %v\n", result2["approved"])
	fmt.Printf("  Messages exchanged: %d\n", len(coordTeam.MessageHistory()))
}
