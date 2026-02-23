# Chronos

A Go-based agentic framework for building highly-scalable, durable AI agents with first-class persistence, observability, and CLI tooling.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     ChronosOS (Control Plane)           │
│  Auth & RBAC │ Tracing & Audit │ Approval │ Dashboard   │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────┐
│                      Engine                             │
│  StateGraph Runtime │ Model Provider │ Tools │ Streaming │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────┐
│                       SDK                               │
│  Agent Builder │ Skills & Plugins │ Memory API          │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────┐
│                   Storage Layer (Pluggable)              │
│  SQLite │ PostgreSQL │ Redis │ MongoDB │ Qdrant │ ...   │
└─────────────────────────────────────────────────────────┘
```

### Package Layout

| Package | Purpose |
|---------|---------|
| `sdk/agent` | Agent definition & builder API |
| `sdk/skill` | Skill/plugin metadata & registry |
| `sdk/memory` | Short-term & long-term memory API |
| `engine/graph` | StateGraph durable execution runtime |
| `engine/model` | Pluggable LLM provider interfaces |
| `engine/tool` | Tool registry with permissions & approval |
| `engine/stream` | SSE event streaming |
| `os/` | ChronosOS control plane (auth, trace, approval) |
| `storage/` | Storage & VectorStore interfaces |
| `storage/adapters/*` | SQLite, PostgreSQL, Qdrant, Redis, etc. |
| `cli/` | Interactive CLI with REPL & commands |
| `sandbox/` | Sandboxed execution for untrusted code |

## Quickstart

### 1. Create an agent (~30 lines)

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/chronos-ai/chronos/engine/graph"
    "github.com/chronos-ai/chronos/sdk/agent"
    "github.com/chronos-ai/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()
    store, _ := sqlite.New("myagent.db")
    defer store.Close()
    store.Migrate(ctx)

    g := graph.New("demo").
        AddNode("greet", func(_ context.Context, s graph.State) (graph.State, error) {
            s["msg"] = fmt.Sprintf("Hello, %s!", s["user"])
            return s, nil
        }).
        SetEntryPoint("greet").
        SetFinishPoint("greet")

    a, err := agent.New("demo", "Demo Agent").WithStorage(store).WithGraph(g).Build()
    if err != nil { log.Fatal(err) }

    result, _ := a.Run(ctx, map[string]any{"user": "World"})
    fmt.Println(result.State["msg"]) // "Hello, World!"
}
```

### 2. Add a skill

```go
import "github.com/chronos-ai/chronos/sdk/skill"

builder.AddSkill(&skill.Skill{
    Name:    "web_search",
    Version: "1.0.0",
    Tools:   []string{"web_search"},
})
```

### 3. Configure PostgreSQL + Qdrant (production)

```go
import (
    "github.com/chronos-ai/chronos/storage/adapters/postgres"
    "github.com/chronos-ai/chronos/storage/adapters/qdrant"
)

store, _ := postgres.New("postgres://user:pass@localhost:5432/chronos")
store.Migrate(ctx)

vectors := qdrant.New("http://localhost:6333")
vectors.CreateCollection(ctx, "embeddings", 1536)
```

### 4. Run ChronosOS

```bash
# Via CLI
go run ./cli/main.go serve :8420

# Via Docker
docker build -f deploy/docker/Dockerfile -t chronos .
docker run -p 8420:8420 chronos
```

### 5. Interactive CLI

```bash
go run ./cli/main.go repl

chronos> /help
chronos> /sessions
chronos> /checkpoints <session_id>
chronos> /quit
```

## Key Features

- **Durable Execution**: StateGraph runtime with checkpoints, interrupts, and resume-from-checkpoint (time-travel debugging)
- **Pluggable Storage**: Single `Storage` interface with adapters for SQLite, PostgreSQL, Redis, MongoDB, DynamoDB
- **Vector Stores**: `VectorStore` interface with Qdrant, RedisVector, Milvus, Weaviate, Pinecone adapters
- **Human-in-the-Loop**: Interrupt nodes and approval API for high-risk tool calls
- **Observability**: Full tracing of node transitions, tool calls, and model responses; SSE streaming
- **Multi-Agent**: Subagent spawning with per-agent isolation and limited privileges
- **Skill System**: Install/uninstall skills with metadata manifests and versioning
- **Sandbox**: Isolated execution for untrusted code (process-based; container support planned)
- **CLI**: Interactive REPL with slash commands, persistent memory, and headless batch mode

## Deployment

Helm chart provided in `deploy/helm/chronos/`:

```bash
helm install chronos deploy/helm/chronos/ \
  --set image.tag=0.1.0 \
  --set storage.backend=postgres
```

## License

Apache 2.0
