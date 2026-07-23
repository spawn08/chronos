package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

// TestResumeDoesNotRerunCompletedNodes verifies the core durability guarantee:
// after a node fails, Resume continues from the last checkpoint and never
// re-executes an already-completed node (nor its LLM call).
func TestResumeDoesNotRerunCompletedNodes(t *testing.T) {
	ctx := context.Background()

	store, err := sqlite.New(filepath.Join(t.TempDir(), "pipeline.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	llm := &stubProvider{}
	runs := map[string]int{}
	failReview := true

	compiled, err := buildGraph(llm, runs, &failReview).Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	const sessionID = "sess-test"

	// First attempt crashes at review. A Runner is single-use, so each
	// execution constructs its own over the same durable store.
	if _, err := graph.NewRunner(compiled, store).Run(ctx, sessionID, graph.State{"topic": "durability"}); err == nil {
		t.Fatal("expected first run to fail at review")
	}

	// Resume after the "crash" with a fresh runner.
	failReview = false
	result, err := graph.NewRunner(compiled, store).Resume(ctx, sessionID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	if result.Status != graph.RunStatusCompleted {
		t.Errorf("status = %q, want completed", result.Status)
	}

	tests := []struct {
		node string
		want int
	}{
		{"draft", 1},    // ran once; its LLM call must NOT repeat on resume
		{"review", 2},   // failed once, then succeeded on resume
		{"finalize", 1}, // ran once after resume
	}
	for _, tc := range tests {
		if runs[tc.node] != tc.want {
			t.Errorf("node %q ran %d times, want %d", tc.node, runs[tc.node], tc.want)
		}
	}

	// LLM was called only where a node body reached provider.Chat: draft (1) +
	// the successful review (1). The failed review returned before its call.
	if llm.calls != 2 {
		t.Errorf("llm calls = %d, want 2", llm.calls)
	}

	if got, _ := result.State["final"].(string); got == "" {
		t.Error("final state missing")
	}
}
