// Example: rag_knowledge — Retrieval-Augmented Generation with the Knowledge API.
//
// What you'll learn:
//   - How to build a RAG pipeline with knowledge.VectorKnowledge
//   - How the Knowledge / VectorStore / EmbeddingsProvider interfaces fit together
//   - How to ingest documents, embed them, and run a similarity search
//   - How retrieved passages ground an LLM answer (optional, needs a provider)
//
// This example is fully OFFLINE by default. It ships a self-contained
// in-memory VectorStore (cosine similarity) and a deterministic, hashing
// EmbeddingsProvider, so retrieval runs with NO API key and NO network. If an
// LLM provider is configured in the environment (OPENAI_API_KEY, etc.) the
// example additionally feeds the retrieved passages to that model to produce a
// grounded answer.
//
// Prerequisites:
//   - Go 1.22+
//   - (optional) an LLM API key for the grounded-answer step
//
// Run:
//
//	go run ./examples/rag_knowledge/          # retrieval only, offline
//	OPENAI_API_KEY=sk-... go run ./examples/rag_knowledge/   # + grounded answer
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/examples/internal/providers"
	"github.com/spawn08/chronos/sdk/knowledge"
)

// embedDimension is the fixed width of the vectors produced by the offline
// hashing embedder. It must match the dimension passed to NewVectorKnowledge.
const embedDimension = 256

func main() {
	ctx := context.Background()

	fmt.Println("━━━ Chronos RAG / Knowledge example ━━━")

	// ════════════════════════════════════════════════════════════════
	// Step 1: Assemble the RAG building blocks
	//
	// VectorKnowledge needs a VectorStore (where embeddings live) and an
	// EmbeddingsProvider (that turns text into vectors). Both are stubbed
	// here so the example runs offline; in production you would swap in the
	// Qdrant adapter and a real embeddings provider.
	// ════════════════════════════════════════════════════════════════
	store := newMemoryVectorStore()
	embedder := newHashEmbedder(embedDimension)

	kb := knowledge.NewVectorKnowledge(
		"chronos-docs", // collection name
		embedDimension, // vector dimension
		store,          // storage.VectorStore
		embedder,       // model.EmbeddingsProvider
		"hash-stub",    // embedding model id (passed through to the embedder)
		knowledge.WithTopK(3),
	)

	// ════════════════════════════════════════════════════════════════
	// Step 2: Ingest a small corpus
	//
	// AddDocuments queues documents; Load embeds and upserts them into the
	// vector store. Load is idempotent and safe to call repeatedly.
	// ════════════════════════════════════════════════════════════════
	kb.AddDocuments(
		knowledge.Document{
			ID:      "goroutines",
			Content: "Goroutines are lightweight threads managed by the Go runtime. They start with the go keyword and use only about two kilobytes of stack, so a program can run hundreds of thousands of them concurrently.",
		},
		knowledge.Document{
			ID:      "channels",
			Content: "Channels are typed conduits that let goroutines send and receive values. They provide synchronization without explicit locks. Create one with make(chan T) and communicate with the arrow operator.",
		},
		knowledge.Document{
			ID:      "interfaces",
			Content: "Interfaces in Go are satisfied implicitly: any type that implements the required methods satisfies the interface automatically, enabling decoupled designs with compile-time safety.",
		},
		knowledge.Document{
			ID:      "context",
			Content: "The context package carries deadlines, cancellation signals, and request-scoped values across API boundaries. Pass a context.Context as the first parameter of I/O functions.",
		},
	)

	if err := kb.Load(ctx); err != nil {
		log.Fatalf("load knowledge: %v", err)
	}
	fmt.Println("Indexed 4 documents into the in-memory vector store.")

	// ════════════════════════════════════════════════════════════════
	// Step 3: Retrieve the most relevant passages
	// ════════════════════════════════════════════════════════════════
	query := "How do lightweight threads work in Go?"
	fmt.Printf("\nQuery: %q\n", query)

	docs, err := kb.Search(ctx, query, 3)
	if err != nil {
		log.Fatalf("search: %v", err)
	}

	fmt.Println("\nTop retrieved passages:")
	for i, d := range docs {
		fmt.Printf("  %d. [%s] score=%.3f — %s\n", i+1, d.ID, d.Score, truncate(d.Content, 80))
	}

	// ════════════════════════════════════════════════════════════════
	// Step 4 (optional): Ground an LLM answer on the retrieved passages
	//
	// Retrieval above needs no credentials. This step only runs when a
	// provider is configured; otherwise the example ends after retrieval.
	// ════════════════════════════════════════════════════════════════
	provider, name := providers.Pick()
	if provider == nil {
		fmt.Println("\nNo LLM provider configured — skipping grounded answer.")
		fmt.Println(providers.EnvHint())
		return
	}

	fmt.Printf("\nGrounding an answer with %s (%s)...\n", name, provider.Model())
	var ctxBuf strings.Builder
	for _, d := range docs {
		ctxBuf.WriteString("- ")
		ctxBuf.WriteString(d.Content)
		ctxBuf.WriteString("\n")
	}

	resp, err := provider.Chat(ctx, &model.ChatRequest{
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "Answer the question using ONLY the provided context. Be concise."},
			{Role: model.RoleUser, Content: fmt.Sprintf("Context:\n%s\nQuestion: %s", ctxBuf.String(), query)},
		},
	})
	if err != nil {
		log.Fatalf("chat: %v", err)
	}
	fmt.Printf("\nAnswer: %s\n", strings.TrimSpace(resp.Content))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
