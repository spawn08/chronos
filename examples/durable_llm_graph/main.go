// Example: durable_llm_graph — where LLM calls happen, and how the StateGraph
// runtime makes them durable.
//
// What you'll learn:
//   - LLM calls happen INSIDE graph nodes (the runtime itself is LLM-agnostic).
//   - The runner checkpoints state after every node that completes.
//   - When a later node fails (a crash, a transient provider/network error, a
//     process restart), Resume continues from the last checkpoint — so a
//     completed, EXPENSIVE LLM node is never re-executed.
//
// The graph is a 3-step content pipeline:
//
//	draft (LLM) ──▶ review (LLM) ──▶ finalize
//
// We deliberately make `review` fail on its first attempt to simulate a crash
// after `draft` has already done its expensive LLM work. On Resume, `draft` is
// skipped (its output was checkpointed) and execution picks up at `review`.
//
// This example uses a deterministic stub Provider so it runs offline and in CI
// with no API keys. Swap it for a real provider (OpenAI/Anthropic/Gemini/Ollama)
// and the graph code does not change — that is the whole point of the
// model.Provider interface. See examples/graph_with_llm for a real-provider version.
//
// Run:
//
//	go run ./examples/durable_llm_graph/
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()

	// Durable store. Checkpoints live here; because they are durable, Resume
	// works even from a brand-new process (or a different replica) pointed at
	// the same database. We use a temp SQLite file to keep the example
	// self-contained; in production use the Postgres adapter.
	dir, err := os.MkdirTemp("", "chronos-durable")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	store, err := sqlite.New(filepath.Join(dir, "pipeline.db"))
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	llm := &stubProvider{}
	// nodeRuns counts how many times each node's body actually executed, so we
	// can prove Resume does not re-run completed nodes.
	nodeRuns := map[string]int{}
	// failReview simulates a crash/transient failure on the first pass; we clear
	// it before Resume so the second attempt succeeds.
	failReview := true

	g := buildGraph(llm, nodeRuns, &failReview)
	compiled, err := g.Compile()
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}

	const sessionID = "sess-durable-001"

	// ── First attempt: draft succeeds, review "crashes" ──────────────────────
	// A Runner is single-use, so each execution gets its own; the durable state
	// lives in the store, not the runner.
	fmt.Println("━━━ Run #1 (draft runs, then review fails) ━━━")
	_, err = graph.NewRunner(compiled, store).Run(ctx, sessionID, graph.State{
		"topic": "durable execution in Chronos",
	})
	if err == nil {
		return errors.New("expected the first run to fail at review")
	}
	fmt.Printf("  run failed as expected: %v\n", err)
	fmt.Printf("  node runs so far: %v\n", nodeRuns)
	fmt.Printf("  llm calls so far: %d\n\n", llm.calls)

	// ── The "crash" is over. Resume from the durable checkpoint. ─────────────
	failReview = false
	fmt.Println("━━━ Resume (continues from the last checkpoint, fresh runner) ━━━")
	result, err := graph.NewRunner(compiled, store).Resume(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("resume: %w", err)
	}

	fmt.Printf("  status:      %s\n", result.Status)
	fmt.Printf("  node runs:   %v\n", nodeRuns)
	fmt.Printf("  llm calls:   %d\n", llm.calls)
	fmt.Printf("  final text:  %v\n\n", result.State["final"])

	if nodeRuns["draft"] != 1 {
		return fmt.Errorf("durability violated: draft ran %d times, want 1 (its LLM call must not repeat on resume)", nodeRuns["draft"])
	}
	fmt.Println("✓ draft (and its expensive LLM call) ran exactly once despite the crash — that is the durability guarantee.")
	return nil
}

// buildGraph wires the draft → review → finalize pipeline. Each node is an
// ordinary Go function; LLM calls happen here, inside the nodes. The graph
// runtime only orchestrates and checkpoints — it never calls an LLM itself.
func buildGraph(llm model.Provider, runs map[string]int, failReview *bool) *graph.StateGraph {
	g := graph.New("content-pipeline")

	// Node 1 — draft: the expensive LLM step we must not repeat on resume.
	g.AddNode("draft", func(ctx context.Context, s graph.State) (graph.State, error) {
		runs["draft"]++
		topic, _ := s["topic"].(string)
		fmt.Printf("  🔵 [draft] calling LLM (expensive) for topic %q\n", topic)
		resp, err := llm.Chat(ctx, &model.ChatRequest{
			Messages: []model.Message{
				{Role: model.RoleSystem, Content: "You are a technical writer. Draft a short paragraph."},
				{Role: model.RoleUser, Content: "Write a draft about: " + topic},
			},
		})
		if err != nil {
			return s, fmt.Errorf("draft llm: %w", err)
		}
		s["draft"] = resp.Content
		return s, nil
	})

	// Node 2 — review: another LLM call; fails on the first attempt.
	g.AddNode("review", func(ctx context.Context, s graph.State) (graph.State, error) {
		runs["review"]++
		if *failReview {
			return s, errors.New("simulated crash: provider timeout during review")
		}
		draft, _ := s["draft"].(string)
		fmt.Printf("  🔵 [review] calling LLM to review the draft\n")
		resp, err := llm.Chat(ctx, &model.ChatRequest{
			Messages: []model.Message{
				{Role: model.RoleSystem, Content: "You are an editor. Improve the draft."},
				{Role: model.RoleUser, Content: draft},
			},
		})
		if err != nil {
			return s, fmt.Errorf("review llm: %w", err)
		}
		s["reviewed"] = resp.Content
		return s, nil
	})

	// Node 3 — finalize: pure data assembly, no LLM.
	g.AddNode("finalize", func(_ context.Context, s graph.State) (graph.State, error) {
		runs["finalize"]++
		reviewed, _ := s["reviewed"].(string)
		s["final"] = "FINAL: " + reviewed
		return s, nil
	})

	g.SetEntryPoint("draft")
	g.AddEdge("draft", "review")
	g.AddEdge("review", "finalize")
	g.SetFinishPoint("finalize")
	return g
}

// stubProvider is a deterministic, offline model.Provider for the example and
// its test. A real provider (OpenAI/Anthropic/Gemini/Ollama) is a drop-in
// replacement — the graph nodes above do not change.
type stubProvider struct{ calls int }

func (p *stubProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	p.calls++
	last := ""
	for _, m := range req.Messages {
		if m.Role == model.RoleUser {
			last = m.Content
		}
	}
	return &model.ChatResponse{
		ID:         fmt.Sprintf("stub-%d", p.calls),
		Content:    "response to: " + last,
		Role:       model.RoleAssistant,
		Usage:      model.Usage{PromptTokens: 12, CompletionTokens: 9},
		StopReason: model.StopReasonEnd,
	}, nil
}

func (p *stubProvider) StreamChat(ctx context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, err := p.Chat(ctx, req)
	if err != nil {
		close(ch)
		return ch, err
	}
	ch <- resp
	close(ch)
	return ch, nil
}

func (p *stubProvider) Name() string  { return "stub" }
func (p *stubProvider) Model() string { return "stub-model-v1" }
