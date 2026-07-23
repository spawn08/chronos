// Azure OpenAI RAG (retrieval-augmented generation) example for Chronos.
//
// This example shows an end-to-end RAG flow on Azure OpenAI:
//
//  1. Build a small self-contained in-memory VectorStore (cosine similarity).
//     Chronos ships production vector adapters (e.g. Qdrant), but there is no
//     built-in in-memory store — so this file doubles as a reference for how
//     to implement the storage.VectorStore interface.
//  2. Wrap it in a knowledge.VectorKnowledge that embeds documents with an
//     Azure OpenAI embeddings deployment (model.NewAzureOpenAIEmbeddings...).
//  3. Load a few sample documents.
//  4. Answer a question by retrieving the most relevant documents and feeding
//     them as grounding context into an Azure chat completion.
//
// Prerequisites:
//   - An Azure OpenAI resource with BOTH a chat deployment (e.g. gpt-4o-mini)
//     and an embeddings deployment (e.g. text-embedding-3-small)
//   - Go 1.24+
//
// Set the following environment variables before running:
//
//	export AZURE_OPENAI_API_KEY=<your-azure-api-key>
//	export AZURE_OPENAI_ENDPOINT=https://<your-resource>.openai.azure.com
//	export AZURE_OPENAI_DEPLOYMENT=<your-chat-deployment>            # chat
//	export AZURE_OPENAI_EMBED_DEPLOYMENT=<your-embeddings-deployment> # embeddings
//	export AZURE_OPENAI_API_VERSION=2024-10-21
//
// Run:
//
//	go run ./examples/azure_rag/main.go
//
// If AZURE_OPENAI_API_KEY is not set the example prints the required variables
// and exits cleanly (0) without making any network call. The in-memory
// VectorStore itself is exercised offline by main_test.go.
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/knowledge"
	"github.com/spawn08/chronos/storage"
)

// embedDimension is the vector size for text-embedding-3-small. Adjust to
// match your embeddings deployment (e.g. 3072 for text-embedding-3-large).
const embedDimension = 1536

func main() {
	fmt.Println("=== Chronos: Azure OpenAI RAG ===")

	// ────────────────────────────────────────────────────────────────
	// Step 1: Resolve Azure configuration; exit gracefully without creds.
	// ────────────────────────────────────────────────────────────────
	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	if apiKey == "" {
		printEnvHelp()
		return
	}

	endpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	apiVersion := os.Getenv("AZURE_OPENAI_API_VERSION")
	chatDeployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
	embedDeployment := os.Getenv("AZURE_OPENAI_EMBED_DEPLOYMENT")

	ctx := context.Background()

	// ────────────────────────────────────────────────────────────────
	// Step 2: Providers — chat and embeddings, both on Azure.
	// ────────────────────────────────────────────────────────────────
	chat := model.NewAzureOpenAIWithConfig(model.AzureConfig{
		ProviderConfig: model.ProviderConfig{APIKey: apiKey, BaseURL: endpoint},
		Deployment:     chatDeployment,
		APIVersion:     apiVersion,
	})
	embedder := model.NewAzureOpenAIEmbeddingsWithConfig(
		model.ProviderConfig{APIKey: apiKey, BaseURL: endpoint},
		embedDeployment,
		apiVersion,
	)
	fmt.Printf("\n[1] Chat deployment: %s | Embeddings deployment: %s\n", chatDeployment, embedDeployment)

	// ────────────────────────────────────────────────────────────────
	// Step 3: Knowledge base backed by the in-memory VectorStore.
	// ────────────────────────────────────────────────────────────────
	store := NewMemoryVectorStore()
	defer store.Close()

	kb := knowledge.NewVectorKnowledge("chronos-docs", embedDimension, store, embedder, embedDeployment)
	kb.AddDocuments(sampleDocuments()...)
	if err := kb.Load(ctx); err != nil {
		log.Fatalf("load knowledge: %v", err)
	}
	fmt.Println("[2] Indexed sample documents into the in-memory vector store")

	// ────────────────────────────────────────────────────────────────
	// Step 4: Retrieve context and answer the question.
	// ────────────────────────────────────────────────────────────────
	question := "How does Chronos make agents durable across crashes?"
	fmt.Printf("\n[3] Question: %s\n", question)

	docs, err := kb.Search(ctx, question, 3)
	if err != nil {
		log.Fatalf("search: %v", err)
	}
	fmt.Printf("[4] Retrieved %d document(s)\n", len(docs))

	answer, err := answerWithContext(ctx, chat, question, docs)
	if err != nil {
		log.Fatalf("answer: %v", err)
	}
	fmt.Printf("\n[5] Answer:\n%s\n", answer)
}

// answerWithContext feeds retrieved documents as grounding context into a chat
// completion, instructing the model to answer only from that context.
func answerWithContext(ctx context.Context, chat model.Provider, question string, docs []knowledge.Document) (string, error) {
	var b strings.Builder
	for i, d := range docs {
		fmt.Fprintf(&b, "[%d] %s\n", i+1, d.Content)
	}

	resp, err := chat.Chat(ctx, &model.ChatRequest{
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "You are a documentation assistant. Answer the question using ONLY the provided context. If the context is insufficient, say so."},
			{Role: model.RoleUser, Content: fmt.Sprintf("Context:\n%s\nQuestion: %s", b.String(), question)},
		},
	})
	if err != nil {
		return "", fmt.Errorf("chat: %w", err)
	}
	return resp.Content, nil
}

// sampleDocuments returns a tiny corpus about Chronos for the demo.
func sampleDocuments() []knowledge.Document {
	return []knowledge.Document{
		{ID: "doc-durability", Content: "Chronos agents run on a durable StateGraph runtime. Every node execution is checkpointed to storage, so if the process crashes the agent resumes from the last completed node instead of restarting."},
		{ID: "doc-storage", Content: "Chronos persistence is pluggable via the storage.Storage interface. SQLite is used for development and PostgreSQL for production. Both persist sessions, checkpoints, and events."},
		{ID: "doc-teams", Content: "Chronos teams orchestrate multiple agents using strategies such as sequential, parallel, router, and coordinator."},
		{ID: "doc-providers", Content: "Chronos supports many model providers behind one Provider interface, including Azure OpenAI, OpenAI, Anthropic, and Gemini. Swapping providers requires no code changes."},
	}
}

// ─────────────────────────────────────────────────────────────────────────
// MemoryVectorStore — a minimal in-memory storage.VectorStore implementation
// using cosine similarity. Reference implementation; not for production use.
// ─────────────────────────────────────────────────────────────────────────

// MemoryVectorStore is a thread-safe in-memory vector store.
type MemoryVectorStore struct {
	mu          sync.RWMutex
	collections map[string][]storage.Embedding
}

// NewMemoryVectorStore creates an empty in-memory vector store.
func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{collections: make(map[string][]storage.Embedding)}
}

// CreateCollection ensures a collection exists. Dimension is not enforced here.
func (m *MemoryVectorStore) CreateCollection(_ context.Context, name string, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.collections[name]; !ok {
		m.collections[name] = nil
	}
	return nil
}

// Upsert inserts or replaces embeddings by ID within a collection.
func (m *MemoryVectorStore) Upsert(_ context.Context, collection string, embeddings []storage.Embedding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing := m.collections[collection]
	for _, e := range embeddings {
		replaced := false
		for i := range existing {
			if existing[i].ID == e.ID {
				existing[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			existing = append(existing, e)
		}
	}
	m.collections[collection] = existing
	return nil
}

// Search returns the top-k embeddings ranked by cosine similarity to query.
func (m *MemoryVectorStore) Search(_ context.Context, collection string, query []float32, topK int) ([]storage.SearchResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := m.collections[collection]
	results := make([]storage.SearchResult, 0, len(items))
	for _, e := range items {
		results = append(results, storage.SearchResult{Embedding: e, Score: cosineSimilarity(query, e.Vector)})
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if topK > 0 && topK < len(results) {
		results = results[:topK]
	}
	return results, nil
}

// Delete removes embeddings by ID from a collection.
func (m *MemoryVectorStore) Delete(_ context.Context, collection string, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	remove := make(map[string]bool, len(ids))
	for _, id := range ids {
		remove[id] = true
	}
	existing := m.collections[collection]
	kept := existing[:0]
	for _, e := range existing {
		if !remove[e.ID] {
			kept = append(kept, e)
		}
	}
	m.collections[collection] = kept
	return nil
}

// Close releases resources. Nothing to release for an in-memory store.
func (m *MemoryVectorStore) Close() error { return nil }

// cosineSimilarity returns the cosine similarity of two vectors, or 0 when
// either is zero-length or a zero vector.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

func printEnvHelp() {
	fmt.Println("\nAZURE_OPENAI_API_KEY is not set — skipping the live Azure call.")
	fmt.Println("Set these environment variables to run against your resource:")
	fmt.Println("  export AZURE_OPENAI_API_KEY=<your-azure-api-key>")
	fmt.Println("  export AZURE_OPENAI_ENDPOINT=https://<your-resource>.openai.azure.com")
	fmt.Println("  export AZURE_OPENAI_DEPLOYMENT=<your-chat-deployment>")
	fmt.Println("  export AZURE_OPENAI_EMBED_DEPLOYMENT=<your-embeddings-deployment>")
	fmt.Println("  export AZURE_OPENAI_API_VERSION=2024-10-21")
}
