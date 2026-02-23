# Chronos — Project Instructions for Claude Code

## Overview
Chronos is a Go-based agentic framework for building durable, scalable AI agents. Module: `github.com/chronos-ai/chronos`.

## Architecture (top-down)
```
ChronosOS (os/)        — Control plane: auth, tracing, approval, dashboard HTTP API
Engine (engine/)       — Runtime: StateGraph execution, model providers, tool registry, streaming
SDK (sdk/)             — User-facing: agent builder, skills, memory, knowledge, teams
Storage (storage/)     — Persistence: pluggable adapters for SQL, NoSQL, and vector stores
```

## Package Map

| Package | Path | Purpose |
|---------|------|---------|
| Agent builder | `sdk/agent/` | Fluent `agent.New().WithModel().WithStorage().Build()` API |
| Skills | `sdk/skill/` | Skill metadata, versioning, registry |
| Memory | `sdk/memory/` | Short/long-term memory + LLM-powered `Manager` |
| Knowledge | `sdk/knowledge/` | RAG: `Knowledge` interface, `VectorKnowledge` impl |
| Teams | `sdk/team/` | Multi-agent orchestration (sequential, parallel, router) |
| StateGraph | `engine/graph/` | Durable graph runtime: nodes, edges, checkpoints, resume |
| Models | `engine/model/` | `Provider` interface, `EmbeddingsProvider`, caching |
| Tools | `engine/tool/` | Tool `Registry` with permissions and approval hooks |
| Hooks | `engine/hooks/` | Before/After middleware `Hook` interface, `Chain` |
| Guardrails | `engine/guardrails/` | Input/output validation `Guardrail` interface |
| Streaming | `engine/stream/` | SSE event `Broker` |
| ChronosOS | `os/` | HTTP control plane server |
| Auth | `os/auth/` | RBAC stubs |
| Tracing | `os/trace/` | Span collector + audit logging |
| Approval | `os/approval/` | Human-in-the-loop approval service |
| Storage | `storage/` | `Storage` and `VectorStore` interfaces |
| SQLite | `storage/adapters/sqlite/` | Full `Storage` impl (dev/test) |
| PostgreSQL | `storage/adapters/postgres/` | Full `Storage` impl (production) |
| Qdrant | `storage/adapters/qdrant/` | `VectorStore` impl via REST |
| CLI | `cli/` | Interactive REPL + headless commands |
| Sandbox | `sandbox/` | Process-level isolation for untrusted code |

## Key Interfaces

When implementing new adapters, providers, or extensions, implement these interfaces:

- **`storage.Storage`** (`storage/storage.go`) — 18 methods for sessions, memory, audit logs, traces, events, checkpoints
- **`storage.VectorStore`** (`storage/vector.go`) — Upsert, Search, Delete, CreateCollection, Close
- **`model.Provider`** (`engine/model/provider.go`) — Chat, StreamChat
- **`model.EmbeddingsProvider`** (`engine/model/embeddings.go`) — Embed
- **`knowledge.Knowledge`** (`sdk/knowledge/knowledge.go`) — Load, Search, Close
- **`guardrails.Guardrail`** (`engine/guardrails/guardrails.go`) — Check(ctx, content) Result
- **`hooks.Hook`** (`engine/hooks/hooks.go`) — Before(ctx, event), After(ctx, event)
- **`sandbox.Sandbox`** (`sandbox/sandbox.go`) — Execute, Close

## Design Patterns

1. **Builder pattern** — `sdk/agent/agent.go`: `agent.New(id, name).WithModel(...).AddTool(...).Build()`
2. **Adapter pattern** — All storage backends implement `storage.Storage`; swap via config
3. **Chain/middleware** — `hooks.Chain` composes multiple `Hook` implementations
4. **Registry pattern** — `tool.Registry` and `skill.Registry` for dynamic registration
5. **Interface segregation** — `Storage` (relational) is separate from `VectorStore` (embeddings)

## Conventions

### Go style
- Package comments on every package (first line of first file)
- Errors: return `fmt.Errorf("context: %w", err)` — always wrap with context
- Constructors: `New(...)` returns `(*T, error)` or `*T` if infallible
- No init() functions; explicit initialization via constructors
- JSON tags on all exported struct fields
- Use `context.Context` as first parameter on all I/O methods

### Naming
- Interfaces: verb or noun (e.g., `Storage`, `Provider`, `Guardrail`) — no `I` prefix
- Implementations: descriptive noun (e.g., `Store`, `Anthropic`, `VectorKnowledge`)
- Files: lowercase, match the primary type (e.g., `runner.go` for `Runner`)

### Storage adapters
- Each adapter lives in `storage/adapters/<name>/`
- Must implement all methods of `storage.Storage` or `storage.VectorStore`
- SQL adapters: use `database/sql` with `?` (SQLite) or `$N` (Postgres) placeholders
- JSON columns: marshal with `encoding/json`, scan into `[]byte` or `string`
- Always provide a `Migrate(ctx)` method for schema creation

### Testing
- Test files: `*_test.go` in the same package
- Use table-driven tests
- For storage adapters: test against `:memory:` (SQLite) or testcontainers

## Build & Run

```bash
# Build everything
go build ./...

# Run quickstart example
go run ./examples/quickstart/main.go

# Run CLI
go run ./cli/main.go help
go run ./cli/main.go repl
go run ./cli/main.go serve :8420

# Tidy modules
go mod tidy
```

## Common Tasks

### Add a new storage adapter
1. Create `storage/adapters/<name>/<name>.go`
2. Implement `storage.Storage` (or `storage.VectorStore`)
3. Include `Migrate()` and `Close()` methods
4. Use `/new-adapter` slash command for scaffolding

### Add a new model provider
1. Create `engine/model/<name>.go`
2. Implement `model.Provider` (Chat + StreamChat)
3. Constructor: `New<Name>(apiKey string) *<Name>`

### Add a new tool
1. Define a `tool.Definition` with Name, Description, Parameters (JSON Schema), Permission, Handler
2. Register via `registry.Register(def)` or `agent.New().AddTool(def)`

### Add a graph node
1. Write a `graph.NodeFunc`: `func(ctx context.Context, state graph.State) (graph.State, error)`
2. Register: `graph.New("id").AddNode("name", fn)` or `.AddInterruptNode("name", fn)` for human-in-the-loop

## Do NOT
- Add `init()` functions
- Use global state or package-level variables (except constants)
- Import `os` in library packages (only in `cli/` and `examples/`)
- Skip error wrapping — always `fmt.Errorf("what: %w", err)`
- Use panic for recoverable errors
- Add dependencies without checking if stdlib suffices first
