Configure agent memory (short-term and long-term with vector recall) for a Chronos agent.

The memory use case is: $ARGUMENTS

## Instructions

1. Read the memory manager at `sdk/memory/manager.go` and the memory store at `sdk/memory/store.go`.

2. Understand the memory architecture:
   - **Short-term memory**: Recent conversation turns stored in the session (automatic)
   - **Long-term memory**: Extracted facts/preferences persisted to storage via `memory.Manager`
   - **Vector-indexed recall**: Semantic search over memories using embeddings for relevant recall
   - **Memory tools**: Built-in `remember`, `forget`, `recall` tools the agent can invoke

3. Create the memory-enabled agent in Go:

```go
package main

import (
    "context"
    "log"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/memory"
    "github.com/spawn08/chronos/storage/adapters/sqlite"
    "github.com/spawn08/chronos/storage/adapters/qdrant"
)

func main() {
    ctx := context.Background()

    // 1. Storage backend
    store, err := sqlite.New("memory-agent.db")
    if err != nil { log.Fatal(err) }
    defer store.Close()
    store.Migrate(ctx)

    // 2. LLM provider (used for memory extraction)
    llm := model.NewAnthropic("${ANTHROPIC_API_KEY}")

    // 3. Memory store
    memStore := memory.NewStore(store)

    // 4. Memory manager — extracts and recalls memories
    mgr := memory.NewManager("agent-1", "user-1", memStore, llm)

    // 5. (Optional) Add vector-indexed recall for semantic search
    embedder := model.NewOpenAIEmbeddings("${OPENAI_API_KEY}", "text-embedding-3-small")
    vectorStore, err := qdrant.New("http://localhost:6333", "agent-memories")
    if err != nil { log.Fatal(err) }
    defer vectorStore.Close()

    mgr = mgr.WithVectorIndex(embedder, vectorStore, "text-embedding-3-small", 1536)

    // 6. Build agent with memory
    a, err := agent.New("memory-agent", "Memory Agent").
        WithModel(llm).
        WithStorage(store).
        WithMemory(mgr).
        Build()
    if err != nil { log.Fatal(err) }

    // The agent now has memory tools: remember, forget, recall
    // Memory extraction happens automatically after each conversation turn
    _ = a
}
```

4. The `memory.Manager` provides three key operations:

   | Method | Purpose |
   |--------|---------|
   | `ExtractMemories(ctx, messages)` | LLM extracts facts/preferences from conversation and stores them |
   | `Recall(ctx, query, topK)` | Retrieves relevant memories (vector search if configured, else keyword) |
   | `MemoryTools()` | Returns tool definitions the agent can call directly |

5. Memory tools exposed to the agent:

   - **remember**: `{"fact": "User prefers dark mode"}` — stores a memory
   - **forget**: `{"query": "dark mode"}` — deletes matching memories
   - **recall**: `{"query": "user preferences", "top_k": 5}` — retrieves relevant memories

6. For YAML-based memory configuration, the agent auto-configures memory when storage is set:

```yaml
agents:
  - id: "assistant"
    name: "Personal Assistant"
    description: "Remembers user preferences across sessions"
    model:
      provider: anthropic
      model: claude-sonnet-4-20250514
      api_key: ${ANTHROPIC_API_KEY}
    storage:
      backend: sqlite
      dsn: "assistant.db"
    system_prompt: |
      You are a personal assistant that remembers user preferences.
      Use the remember tool to store important facts about the user.
      Use the recall tool at the start of conversations to retrieve context.
    num_history_runs: 10
    stream: true
```

7. For production with vector recall, set up embeddings and a vector store:
   - Ensure the vector store is running (e.g., `docker run -p 6333:6333 qdrant/qdrant`)
   - The embeddings provider needs an API key in the environment
   - Dimension must match the embedding model (1536 for text-embedding-3-small)

8. Memory is scoped by `agentID` + `userID` — each user gets their own memory namespace. This enables multi-tenant memory out of the box.

9. Run `go build ./...` to verify compilation.
