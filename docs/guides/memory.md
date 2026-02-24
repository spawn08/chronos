---
title: "Memory & Knowledge"
permalink: /guides/memory/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

Chronos provides two complementary mechanisms for context: **Memory** (user-specific facts and session state) and **Knowledge** (RAG over documents). Both are automatically injected into the agent's context during `Chat` and `ChatWithSession`.

## Memory Store

The `memory.Store` provides short-term (session-scoped) and long-term (persistent) memory on top of `storage.Storage`.

### Creating a Store

```go
store := memory.NewStore(agentID, backend)
```

`backend` must implement `storage.Storage` (e.g., SQLite or PostgreSQL adapter).

### Short-Term Memory

Session-scoped working memory. Values are tied to a session and typically cleared when the session ends.

```go
err := memStore.SetShortTerm(ctx, sessionID, "current_topic", "weather")
if err != nil {
    log.Fatal(err)
}

// List all short-term memories for this agent
records, err := memStore.ListShortTerm(ctx)
```

### Long-Term Memory

Cross-session persistent memory. Use for facts about the user that should persist across conversations.

```go
err := memStore.SetLongTerm(ctx, "user_preference_timezone", "America/Los_Angeles")
if err != nil {
    log.Fatal(err)
}

// List all long-term memories
records, err := memStore.ListLongTerm(ctx)
```

### Retrieval

```go
value, err := memStore.Get(ctx, "user_preference_timezone")
```

## MemoryRecord

Under the hood, memories are stored as `storage.MemoryRecord`:

| Field | Type | Description |
|-------|------|--------------|
| `ID` | string | Unique record ID |
| `SessionID` | string | Session ID (empty for long-term) |
| `AgentID` | string | Agent identifier |
| `UserID` | string | Optional user scope |
| `Kind` | string | `"short_term"` or `"long_term"` |
| `Key` | string | Memory key |
| `Value` | any | Stored value (JSON-serializable) |
| `CreatedAt` | time.Time | Creation timestamp |

## Memory Manager

The `memory.Manager` uses an LLM to autonomously extract facts from conversations and store them as long-term memories. It also formats memories for context injection.

### Creating a Manager

```go
mgr := memory.NewManager(agentID, userID, memStore, provider)
```

`provider` is a `model.Provider` used for extraction and optimization. The manager calls it to decide what to remember.

### ExtractMemories

Analyzes a conversation and stores memorable facts as long-term memories. The LLM returns a JSON array of `{key, value}` objects; only clear, factual information is extracted.

```go
messages := []model.Message{
    {Role: model.RoleUser, Content: "My name is Alice and I live in Berlin."},
    {Role: model.RoleAssistant, Content: "Nice to meet you, Alice!"},
}
err := mgr.ExtractMemories(ctx, messages)
```

The agent automatically calls `ExtractMemories` after each `Chat` and `ChatWithSession` turn when a `MemoryManager` is configured.

### GetUserMemories

Returns all long-term memories formatted for context injection. The agent uses this to prepend "User memories:" to the system context.

```go
memCtx, err := mgr.GetUserMemories(ctx)
// memCtx: "User memories:\n- user_name: Alice\n- user_location: Berlin\n"
```

### OptimizeMemories

Asks the LLM to deduplicate and compress existing long-term memories. Useful when the memory store grows large.

```go
err := mgr.OptimizeMemories(ctx)
```

Runs only when there are at least 5 long-term memories.

## Agentic Memory Tools

`MemoryTools()` returns tool definitions that let the model manage memory directly during conversation: `remember`, `forget`, and `recall`.

Each `MemoryTool` has `Name`, `Description`, and `Handler`. Convert to `tool.Definition` by adding `Parameters` (JSON Schema) and `Permission`:

```go
builder := agent.New("agent", "Agent").WithModel(provider).WithMemoryManager(mgr)
for _, mt := range mgr.MemoryTools() {
    var params map[string]any
    switch mt.Name {
    case "remember":
        params = map[string]any{
            "type": "object",
            "properties": map[string]any{
                "key":   map[string]any{"type": "string", "description": "Memory key"},
                "value": map[string]any{"type": "string", "description": "Fact to store"},
            },
            "required": []string{"key", "value"},
        }
    case "forget":
        params = map[string]any{
            "type": "object",
            "properties": map[string]any{
                "key": map[string]any{"type": "string", "description": "Memory key to remove"},
            },
            "required": []string{"key"},
        }
    default: // recall
        params = map[string]any{"type": "object", "properties": map[string]any{}}
    }
    builder = builder.AddTool(&tool.Definition{
        Name:        mt.Name,
        Description: mt.Description,
        Parameters:  params,
        Permission:  tool.PermAllow,
        Handler:     mt.Handler,
    })
}
a, err := builder.Build()
```

The `remember` tool stores a fact; `forget` removes by key; `recall` lists all stored memories.

## Knowledge (RAG)

The `knowledge.Knowledge` interface supports document indexing and similarity search for RAG.

### Interface

```go
type Knowledge interface {
    Load(ctx context.Context) error
    Search(ctx context.Context, query string, topK int) ([]Document, error)
    Close() error
}
```

| Method | Description |
|--------|--------------|
| `Load` | Index all documents (idempotent) |
| `Search` | Return top-k relevant documents for a query |
| `Close` | Release resources |

### Document

```go
type Document struct {
    ID       string
    Content  string
    Metadata map[string]any
    Score    float32
}
```

### VectorKnowledge

`VectorKnowledge` implements `Knowledge` using a `storage.VectorStore` and `model.EmbeddingsProvider`:

```go
kb := knowledge.NewVectorKnowledge(
    "docs",           // collection name
    1536,             // embedding dimension (e.g., text-embedding-3-small)
    vectorStore,
    embedder,
    "text-embedding-3-small",
)

kb.AddDocuments(
    knowledge.Document{ID: "1", Content: "Chronos is a Go-based agentic framework."},
    knowledge.Document{ID: "2", Content: "Agents use the builder pattern for configuration."},
)

err := kb.Load(ctx)
if err != nil {
    log.Fatal(err)
}

docs, err := kb.Search(ctx, "How do I configure an agent?", 5)
```

### Automatic Injection

When an agent has `Knowledge` configured, `Chat` and `ChatWithSession` automatically:

1. Call `Search(ctx, userMessage, 5)` with the user's message
2. Prepend "Relevant knowledge:" and the top documents to the system context
3. Let the model use this context when generating the response

## Storage Integration

Memory records are persisted via `storage.Storage`. Implementations must support:

- `PutMemory(ctx, *MemoryRecord)` — upsert a memory
- `GetMemory(ctx, agentID, key)` — fetch by agent and key
- `ListMemory(ctx, agentID, kind)` — list by agent and kind (short_term/long_term)
- `DeleteMemory(ctx, id)` — remove by ID

SQLite and PostgreSQL adapters implement these methods.

## Complete Example: Memory and Knowledge

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/knowledge"
    "github.com/spawn08/chronos/sdk/memory"
    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()

    store, err := sqlite.New("memory.db")
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()
    if err := store.Migrate(ctx); err != nil {
        log.Fatal(err)
    }

    provider := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

    memStore := memory.NewStore("demo-agent", store)
    memMgr := memory.NewManager("demo-agent", "user-1", memStore, provider)

    // Optional: add some long-term memories manually
    _ = memStore.SetLongTerm(ctx, "favorite_color", "blue")

    // Optional: RAG knowledge base (requires VectorStore + EmbeddingsProvider)
    // kb := knowledge.NewVectorKnowledge(...)
    // kb.AddDocuments(...)
    // _ = kb.Load(ctx)

    a, err := agent.New("demo-agent", "Demo Agent").
        WithModel(provider).
        WithStorage(store).
        WithMemory(memStore).
        WithMemoryManager(memMgr).
        // WithKnowledge(kb).
        WithSystemPrompt("You are a helpful assistant. Use the user's memories to personalize responses.").
        Build()
    if err != nil {
        log.Fatal(err)
    }

    // First turn: user shares a fact
    resp1, err := a.ChatWithSession(ctx, "session-1", "My name is Bob and I work in San Francisco.")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp1.Content)

    // Second turn: agent recalls the fact (extracted by MemoryManager)
    resp2, err := a.ChatWithSession(ctx, "session-1", "What do you know about me?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp2.Content)
}
```

## Complete Example: VectorKnowledge with Qdrant

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/knowledge"
    "github.com/spawn08/chronos/storage/adapters/qdrant"
)

func main() {
    ctx := context.Background()

    qdrantStore := qdrant.New("http://localhost:6333")
    defer qdrantStore.Close()

    embedder := model.NewOpenAIEmbeddings(os.Getenv("OPENAI_API_KEY"))
    provider := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

    kb := knowledge.NewVectorKnowledge(
        "docs",
        1536,
        qdrantStore,
        embedder,
        "text-embedding-3-small",
    )
    defer kb.Close()

    kb.AddDocuments(
        knowledge.Document{ID: "1", Content: "Chronos agents use the builder pattern."},
        knowledge.Document{ID: "2", Content: "Memory is stored in short-term and long-term stores."},
        knowledge.Document{ID: "3", Content: "VectorKnowledge uses embeddings for RAG search."},
    )
    if err := kb.Load(ctx); err != nil {
        log.Fatal(err)
    }

    a, err := agent.New("rag-agent", "RAG Agent").
        WithModel(provider).
        WithKnowledge(kb).
        WithSystemPrompt("Answer using the provided knowledge. Cite sources when relevant.").
        Build()
    if err != nil {
        log.Fatal(err)
    }

    resp, err := a.Chat(ctx, "How does Chronos handle memory?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Content)
}
```
