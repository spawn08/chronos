---
name: chronos-sdk
description: Complete Chronos Go SDK reference — agent builder API, memory system, RAG/knowledge, tools, MCP, streaming, model providers, and storage backends. The authoritative API reference for programmatic Chronos integration.
---

# Chronos SDK Reference

## Activation
Use this skill when:
- Building Chronos agents programmatically in Go (not YAML)
- Developer needs exact method signatures, constructors, or interfaces
- Wiring up memory, RAG, tools, MCP, streaming, or providers in Go code
- Looking up available storage backends or model providers

## Module
```
go get github.com/spawn08/chronos
```

---

## 1. Agent Builder (`sdk/agent`)

```go
import "github.com/spawn08/chronos/sdk/agent"

a, err := agent.New("id", "name").       // constructor
    Description("what it does").
    WithUserID("user-1").
    // Model
    WithModel(provider).
    WithReasoningModel(reasoningProvider).
    WithReasoningConfig(model.ReasoningConfig{Strategy: "cot", Effort: "high"}).
    // Storage & Memory
    WithStorage(store).
    WithMemory(memStore).
    WithMemoryManager(memMgr).
    WithMemoryRecall(agent.RecallConfig{...}).
    WithKnowledge(kb).
    // Behavior
    WithSystemPrompt("You are...").
    AddInstruction("Be concise").
    WithInstructionsFn(dynamicInstructionsFn).
    AddExample("input", "output").
    AddCapability("code_generation").
    WithOutputSchema(jsonSchema).
    WithHistoryRuns(5).
    WithMaxIterations(10).
    // Tools & MCP
    AddTool(toolDef).
    AddToolkit(toolkit).
    AddMCPServer(mcpConfig).
    // Skills & Sub-Agents
    AddSkill(skill).
    AddSubAgent(subAgent).
    // Middleware
    AddHook(hook).
    AddInputGuardrail("name", guardrail).
    AddOutputGuardrail("name", guardrail).
    // Graph & Streaming
    WithGraph(stateGraph).
    WithStreaming(true).
    WithBroker(broker).
    // Context & Tracing
    WithContextConfig(agent.ContextConfig{MaxTokens: 8192, SummarizeThreshold: 6000, PreserveRecentTurns: 4}).
    WithContextPins(pinsFn).
    WithTracer(collector).
    WithDebug(false).
    Build()  // → (*Agent, error)
```

### Agent Execution
```go
resp, err := a.Chat(ctx, "message")            // single turn → *ChatResponse
ch, err := a.ChatStream(ctx, "message")        // streaming → <-chan *ChatResponse
output, err := a.Execute(ctx, "task")           // text in/out → string
result, err := a.Run(ctx, map[string]any{...})  // graph-based → *RunState
result, err := a.Resume(ctx, sessionID)         // resume paused graph
err = a.ConnectMCP(ctx)                         // connect MCP servers
a.CloseMCP()                                    // disconnect
```

### Load from YAML
```go
cfg, err := agent.LoadFile("agents.yaml")
agents, err := agent.BuildAll(ctx, cfg)
a, err := agent.BuildAgent(ctx, &agentConfig)
provider, err := agent.BuildProvider(modelConfig)
```

---

## 2. Model Providers (`engine/model`)

### Provider Interface
```go
type Provider interface {
    Chat(ctx, *ChatRequest) (*ChatResponse, error)
    StreamChat(ctx, *ChatRequest) (<-chan *ChatResponse, error)
    Name() string
    Model() string
}
```

### Constructors
| Provider | Constructor |
|----------|-------------|
| OpenAI | `model.NewOpenAI(apiKey)` / `NewOpenAIWithConfig(cfg)` |
| Anthropic | `model.NewAnthropic(apiKey)` / `NewAnthropicWithConfig(cfg)` |
| Gemini | `model.NewGemini(apiKey)` / `NewGeminiWithConfig(cfg)` |
| Mistral | `model.NewMistral(apiKey)` / `NewMistralWithConfig(cfg)` |
| Ollama | `model.NewOllama(host, modelName)` / `NewOllamaWithConfig(cfg)` |
| Azure | `model.NewAzureOpenAI(endpoint, apiKey, deployment)` |
| Groq | `model.NewGroq(apiKey, modelID)` |
| Together | `model.NewTogether(apiKey, modelID)` |
| DeepSeek | `model.NewDeepSeek(apiKey, modelID)` |
| OpenRouter | `model.NewOpenRouter(apiKey, modelID)` |
| Fireworks | `model.NewFireworks(apiKey, modelID)` |
| Perplexity | `model.NewPerplexity(apiKey, modelID)` |
| Cohere | `model.NewCohere(apiKey, modelID)` |
| Bedrock | `model.NewBedrock(region, accessKey, secretKey, modelID)` |
| Compatible | `model.NewOpenAICompatible(name, baseURL, apiKey, modelID)` |
| Fallback | `model.NewFallbackProvider(providers...)` |

### Embeddings
```go
type EmbeddingsProvider interface {
    Embed(ctx, *EmbeddingRequest) (*EmbeddingResponse, error)
}
```
`model.NewOpenAIEmbeddings(apiKey)`, `NewAzureOpenAIEmbeddings(endpoint, apiKey, deployment)`, `NewGoogleEmbeddings(apiKey, model)`, `NewOllamaEmbeddings(baseURL, model)`, `NewCohereEmbeddings(apiKey, model)`, `NewCachedEmbeddings(inner)`

---

## 3. Memory (`sdk/memory`)

```go
import "github.com/spawn08/chronos/sdk/memory"

memStore := memory.NewStore(store)
mgr := memory.NewManager("agentID", "userID", memStore, llmProvider)
mgr = mgr.WithVectorIndex(embedder, vectorStore, "model-name", 1536)
mgr = mgr.WithUserID("other-user")

mgr.ExtractMemories(ctx, messages)      // LLM extracts facts → stores
mgr.Recall(ctx, "query", 5)             // semantic search → []RecalledMemory
mgr.OptimizeMemories(ctx)               // deduplicate
mgr.GetUserMemories(ctx)                // all memories as string
mgr.MemoryTools()                       // []MemoryTool: remember, forget, recall
mgr.CanRecall()                         // true if vector index set
```

Scoped by (agentID, userID) — multi-tenant by default.

---

## 4. Knowledge / RAG (`sdk/knowledge`)

```go
import "github.com/spawn08/chronos/sdk/knowledge"

type Knowledge interface {
    Load(ctx) error
    Search(ctx, query string, topK int) ([]Document, error)
    Close() error
}

type Document struct {
    ID string; Content string; Metadata map[string]any; Score float32
}

kb := knowledge.NewVectorKnowledge(collection, dimension, vectorStore, embedder, embedModel,
    knowledge.WithTopK(5),
    knowledge.WithScoreThreshold(0.7),
    knowledge.WithChunking(512, 64),
    knowledge.WithEmbedBatchSize(100),
    knowledge.WithQueryCache(1000, 5*time.Minute),
)
```

Document loaders: `loaders/text.go`, `loaders/csv.go`, `loaders/json.go`, `loaders/pdf.go`, `loaders/web.go`, `loaders/chunker.go`

---

## 5. Tools (`engine/tool`)

```go
import "github.com/spawn08/chronos/engine/tool"

def := &tool.Definition{
    Name:        "tool_name",
    Description: "what it does",
    Parameters:  map[string]any{...}, // JSON Schema
    Permission:  tool.PermAllow,       // PermAllow | PermRequireApproval | PermDeny
    Handler:     func(ctx, args map[string]any) (any, error) { ... },
}

registry := tool.NewRegistry()
registry.Register(def)
registry.SetPermissionMode(tool.PermissionModePrompt) // Prompt | AutoApprove | Deny
registry.SetApprovalHandler(func(ctx, name, args) (bool, error) { ... })
```

Built-in tools: `shell`, `shell_auto`, `file_read`, `file_write`, `file_list`, `file_glob`, `file_grep`

---

## 6. MCP (`engine/mcp`)

```go
import "github.com/spawn08/chronos/engine/mcp"

cfg := mcp.ServerConfig{
    Name: "server", Transport: "stdio", // or "sse"
    Command: "npx", Args: []string{"-y", "@pkg/server"},
    URL: "",              // SSE only
    Permission: "allow",  // allow | require_approval | deny
}
```

---

## 7. Streaming (`engine/stream`)

```go
import "github.com/spawn08/chronos/engine/stream"

broker := stream.NewBroker(
    stream.WithMaxSubscribers(100),
    stream.WithBufferSize(256),
    stream.WithHeartbeat(30 * time.Second),
)
ch := broker.Subscribe("client-id")
defer broker.Unsubscribe("client-id")
broker.Publish(stream.Event{Type: "message", Data: "hello"})
http.Handle("/events", broker.SSEHandler("topic"))
broker.Close()
```

---

## 8. Storage Backends

### Relational (implement `storage.Storage`)
| Backend | Constructor |
|---------|-------------|
| SQLite | `sqlite.New(dsn)` |
| PostgreSQL | `postgres.New(dsn)` |
| In-Memory | `memory.New()` |
| Redis | `redis.New(addr, password, db)` |
| MongoDB | `mongo.New(uri, database)` |
| DynamoDB | `dynamo.New(endpoint, table, region, accessKey, secretKey)` |

### Vector (implement `storage.VectorStore`)
| Backend | Constructor |
|---------|-------------|
| Qdrant | `qdrant.New(baseURL)` |
| pgvector | `pgvector.New(db)` |
| ChromaDB | `chromadb.New(baseURL)` |
| Pinecone | `pinecone.New(host, apiKey)` |
| LanceDB | `lancedb.New(baseURL, apiKey, dbName)` |
| Milvus | `milvus.New(endpoint, token)` |
| Weaviate | `weaviate.New(endpoint, apiKey)` |
| Redis | `redisvector.New(addr)` |

### Storage Interface (22 methods)
Sessions: `Create/Get/Update/ListSessions`
Memory: `Put/Get/List/DeleteMemory`
Audit: `Append/ListAuditLogs`
Traces: `Insert/Get/ListTraces`
Events: `Append/ListEvents`
Checkpoints: `Save/Get/GetLatest/ListCheckpoints`
Lifecycle: `Migrate`, `Close`
