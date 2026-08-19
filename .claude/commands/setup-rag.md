Set up a RAG (Retrieval-Augmented Generation) pipeline for a Chronos agent.

The knowledge domain is: $ARGUMENTS

## Instructions

1. Read the knowledge interface at `sdk/knowledge/knowledge.go` and the vector implementation at `sdk/knowledge/vector.go`.

2. Choose a vector store backend based on the use case:

   | Backend | Best For | Package |
   |---------|----------|---------|
   | Qdrant | Production, self-hosted | `storage/adapters/qdrant/` |
   | PostgreSQL (pgvector) | Already using Postgres | `storage/adapters/postgres/` |
   | SQLite (dev) | Local development, testing | `storage/adapters/sqlite/` |

3. Choose an embeddings provider:

   | Provider | Model | Dimensions |
   |----------|-------|------------|
   | OpenAI | text-embedding-3-small | 1536 |
   | OpenAI | text-embedding-3-large | 3072 |
   | Anthropic | (via compatible endpoint) | varies |
   | Ollama | nomic-embed-text | 768 |
   | Azure OpenAI | text-embedding-ada-002 | 1536 |

4. Create the RAG pipeline Go code:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/spawn08/chronos/engine/graph"
    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/knowledge"
    "github.com/spawn08/chronos/storage/adapters/qdrant"
    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()

    // 1. Storage for sessions/memory
    store, err := sqlite.New("rag-agent.db")
    if err != nil { log.Fatal(err) }
    defer store.Close()
    store.Migrate(ctx)

    // 2. Embeddings provider
    embedder := model.NewOpenAIEmbeddings(
        "${OPENAI_API_KEY}",
        "text-embedding-3-small",
    )

    // 3. Vector store
    vectorStore, err := qdrant.New("http://localhost:6333", "knowledge-collection")
    if err != nil { log.Fatal(err) }
    defer vectorStore.Close()

    // 4. Knowledge source
    kb := knowledge.NewVectorKnowledge(knowledge.VectorConfig{
        Store:      vectorStore,
        Embedder:   embedder,
        Collection: "knowledge-collection",
        Dimension:  1536,
    })

    // 5. Load documents
    if err := kb.Load(ctx); err != nil {
        log.Fatal(err)
    }

    // 6. Build the RAG graph node
    ragNode := func(ctx context.Context, state graph.State) (graph.State, error) {
        query, _ := state["query"].(string)
        docs, err := kb.Search(ctx, query, 5) // top-5 results
        if err != nil {
            return state, fmt.Errorf("knowledge search: %w", err)
        }
        var context string
        for _, doc := range docs {
            context += doc.Content + "\n---\n"
        }
        state["knowledge_context"] = context
        return state, nil
    }

    // 7. Build agent with RAG node
    g := graph.New("rag-graph")
    g.AddNode("retrieve", ragNode)
    g.AddNode("generate", generateNode) // your LLM node
    g.AddEdge("retrieve", "generate")
    g.SetEntryPoint("retrieve")
    g.SetFinishPoint("generate")

    a, err := agent.New("rag-agent", "RAG Agent").
        WithModel(model.NewAnthropic("${ANTHROPIC_API_KEY}")).
        WithStorage(store).
        WithGraph(g).
        Build()
    if err != nil { log.Fatal(err) }

    result, err := a.Run(ctx, "What is...")
    if err != nil { log.Fatal(err) }
    fmt.Println(result)
}
```

5. For YAML-based RAG setup, define tools that wrap knowledge search:

```yaml
agents:
  - id: "rag-agent"
    name: "Knowledge Agent"
    description: "Answers questions using a knowledge base"
    model:
      provider: anthropic
      model: claude-sonnet-4-20250514
      api_key: ${ANTHROPIC_API_KEY}
    system_prompt: |
      You answer questions using the provided knowledge context.
      Always cite which document your answer comes from.
      If the context doesn't contain the answer, say so.
    tools:
      - name: "search_knowledge"
        description: "Search the knowledge base for relevant documents"
        parameters:
          type: object
          properties:
            query:
              type: string
              description: "Search query"
            top_k:
              type: integer
              description: "Number of results (default 5)"
          required: ["query"]
        permission: "allow"
```

6. If the user wants to load documents from files, show how to create a document loader:

```go
func loadDocuments(ctx context.Context, kb knowledge.Knowledge, dir string) error {
    files, err := filepath.Glob(filepath.Join(dir, "*.txt"))
    if err != nil {
        return fmt.Errorf("glob documents: %w", err)
    }
    for _, f := range files {
        data, err := os.ReadFile(f)
        if err != nil {
            return fmt.Errorf("read %s: %w", f, err)
        }
        // Chunk the document for better retrieval
        chunks := chunkText(string(data), 512, 64) // size, overlap
        for i, chunk := range chunks {
            doc := knowledge.Document{
                ID:      fmt.Sprintf("%s-chunk-%d", filepath.Base(f), i),
                Content: chunk,
                Metadata: map[string]any{
                    "source": filepath.Base(f),
                    "chunk":  i,
                },
            }
            if err := kb.Store(ctx, doc); err != nil {
                return fmt.Errorf("store chunk: %w", err)
            }
        }
    }
    return nil
}
```

7. Run `go build ./...` to verify compilation.

8. Provide a test query to validate the pipeline works end-to-end:
```bash
go run ./path/to/main.go
```
