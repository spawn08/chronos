# Chronos Framework Developer Skill

## Description
Specialist skill for implementing features, fixing bugs, and writing tests in the Chronos Go AI agents framework.

## Activation
Use this skill when:
- Implementing features from ROADMAP.md (P0, P1, P2, P3 items)
- Fixing bugs in any Chronos package
- Writing or improving unit/integration tests
- Adding storage adapters, model providers, hooks, guardrails, or tools
- Refactoring existing Chronos code

## Project Context
- **Module**: `github.com/spawn08/chronos`
- **Language**: Go 1.24
- **Dependencies**: Minimal (sqlite3, yaml.v3 only)
- **Test runner**: `go test ./...`
- **Build check**: `go build ./...`

## Architecture Layers
```
os/           → HTTP control plane, auth, tracing, approval
engine/       → Graph runtime, model providers, tools, hooks, guardrails, streaming
sdk/          → Agent builder, skills, memory, knowledge, teams, protocol bus
storage/      → Storage + VectorStore interfaces, adapter implementations
sandbox/      → Process and container code execution isolation
cli/          → REPL and CLI commands
```

## Implementation Workflow

### Before Starting
1. Read `ROADMAP.md` to find the item and its acceptance criteria
2. Read `CLAUDE.md` for project conventions
3. Read the source files being modified
4. Read the relevant interface definitions
5. Read existing tests in the same package

### During Implementation
1. Follow Go conventions (error wrapping, context.Context, no init(), no globals)
2. Write tests alongside implementation (not after)
3. Run `go build ./...` after each significant change
4. Run `go test ./...` to verify no regressions

### After Implementation
1. Run full test suite: `go test ./...`
2. Run build check: `go build ./...`
3. Update `ROADMAP.md`: change `[ ]` to `[x]`, append `<!-- done: YYYY-MM-DD -->`
4. Add entry to Completion Log table in ROADMAP.md

## Key Interfaces Reference

### storage.Storage (18 methods)
```go
CreateSession, GetSession, UpdateSession, ListSessions
PutMemory, GetMemory, ListMemory, DeleteMemory
AppendAuditLog, ListAuditLogs
InsertTrace, GetTrace, ListTraces
AppendEvent, ListEvents
SaveCheckpoint, GetCheckpoint, GetLatestCheckpoint, ListCheckpoints
Migrate, Close
```

### storage.VectorStore (5 methods)
```go
Upsert, Search, Delete, CreateCollection, Close
```

### model.Provider
```go
Chat(ctx, *ChatRequest) (*ChatResponse, error)
StreamChat(ctx, *ChatRequest) (<-chan *ChatResponse, error)
Name() string
Model() string
```

### hooks.Hook
```go
Before(ctx, *Event) error
After(ctx, *Event) error
```

## Testing Standards
- Table-driven tests with `t.Run(name, func(t *testing.T) {...})`
- Test success paths AND failure/edge cases
- Mock providers implement interfaces with deterministic behavior
- For storage: use in-memory or `:memory:` SQLite
- For model providers: use mock that returns canned responses
- Assert with `t.Errorf` / `t.Fatalf` — no external assertion library needed
- Minimum: 80% coverage for critical paths

## Conventions Enforced
- `fmt.Errorf("context: %w", err)` — ALWAYS wrap errors
- `context.Context` as first parameter on I/O methods
- No `init()` functions, no package-level mutable state
- No `os` import in library packages (only `cli/` and `examples/`)
- JSON tags on all exported struct fields
- Interface names are nouns (Storage, Provider) — no `I` prefix
