// Example: semantic_recall — automatic semantic long-term recall (WC-D-001).
//
// What you'll learn:
//   - How to attach a semantic index to a memory.Manager with WithVectorIndex
//   - How memories written in one session are recalled by relevance in another
//   - How an agent auto-injects the top-k relevant memories each turn via
//     WithMemoryRecall (default on when a MemoryManager has a vector index)
//   - How tenant scope keeps one user's memories from leaking to another
//
// This example is fully OFFLINE: it ships a self-contained in-memory VectorStore
// (cosine similarity) and a deterministic hashing EmbeddingsProvider, so it runs
// with NO API key and NO network. In production, swap in the Qdrant adapter
// (storage/adapters/qdrant) and a real embeddings provider.
//
// Run:
//
//	go run ./examples/semantic_recall/
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/spawn08/chronos/sdk/memory"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

// embedDimension is the fixed width of the offline hashing embedder's vectors.
const embedDimension = 256

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	fmt.Println("━━━ Chronos semantic recall example ━━━")

	// Durable relational store for the memory records (in-memory SQLite here).
	store, err := sqlite.New(":memory:")
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	defer store.Close()
	if merr := store.Migrate(ctx); merr != nil {
		return fmt.Errorf("migrate: %w", merr)
	}

	// The semantic index: an EmbeddingsProvider + a VectorStore (both offline).
	embedder := newHashEmbedder(embedDimension)
	vstore := newMemoryVectorStore()

	// ── Session 1: Alice tells the assistant a few facts ──────────────────
	// A nil model provider is fine here: we seed memories directly through the
	// `remember` tool rather than LLM extraction, keeping the example offline.
	writer := memory.NewManager("assistant", "alice", memory.NewStore("assistant", store), nil).
		WithVectorIndex(embedder, vstore, "", embedDimension)

	facts := map[string]string{
		"favorite_food":  "loves spicy Thai green curry",
		"favorite_color": "favorite color is teal",
		"pet":            "has a golden retriever named Biscuit",
		"hometown":       "grew up in Wellington, New Zealand",
	}
	for key, value := range facts {
		if rerr := remember(ctx, writer, key, value); rerr != nil {
			return fmt.Errorf("remember %q: %w", key, rerr)
		}
	}
	fmt.Printf("Session 1: stored %d memories for alice\n\n", len(facts))

	// ── Session 2: a brand-new manager (new process/session) recalls ──────
	reader := memory.NewManager("assistant", "alice", memory.NewStore("assistant", store), nil).
		WithVectorIndex(embedder, vstore, "", embedDimension)

	// The offline embedder matches on shared vocabulary (it is a lexical
	// bag-of-words stub); a real embeddings provider would match true meaning,
	// e.g. "dog" ↔ "golden retriever".
	for _, query := range []string{
		"what food does alice like to eat?",
		"what pet does alice have?",
	} {
		recalled, rerr := reader.Recall(ctx, query, 2)
		if rerr != nil {
			return fmt.Errorf("recall: %w", rerr)
		}
		fmt.Printf("Query: %q\n", query)
		for i := range recalled {
			fmt.Printf("  ↳ [%.3f] %s\n", recalled[i].Score, recalled[i].Content)
		}
		fmt.Println()
	}

	// ── Tenant isolation: Bob never sees Alice's memories ─────────────────
	bob := memory.NewManager("assistant", "bob", memory.NewStore("assistant", store), nil).
		WithVectorIndex(embedder, vstore, "", embedDimension)
	bobRecall, berr := bob.Recall(ctx, "what food does alice like to eat?", 5)
	if berr != nil {
		return fmt.Errorf("bob recall: %w", berr)
	}
	fmt.Printf("Bob recalls %d of alice's memories (want 0 — tenant isolation)\n", len(bobRecall))
	return nil
}

// remember stores one fact through the manager's agentic `remember` tool, which
// both persists it and mirrors it into the semantic index.
func remember(ctx context.Context, m *memory.Manager, key, value string) error {
	for _, tool := range m.MemoryTools() {
		if tool.Name == "remember" {
			_, err := tool.Handler(ctx, map[string]any{"key": key, "value": value})
			return err
		}
	}
	return fmt.Errorf("remember tool unavailable")
}
