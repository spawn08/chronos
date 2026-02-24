<p align="center">
  <h1 align="center">Chronos</h1>
  <p align="center">
    A Go framework for building durable, scalable AI agents.
    <br />
    Define agents. Connect any LLM. Let them collaborate.
  </p>
  <p align="center">
    <a href="#quickstart">Quickstart</a> &middot;
    <a href="#agent-configuration-yaml">YAML Config</a> &middot;
    <a href="#architecture">Architecture</a> &middot;
    <a href="#model-providers">Providers</a> &middot;
    <a href="#cli">CLI</a> &middot;
    <a href="#multi-agent-protocol">Protocol</a> &middot;
    <a href="#deployment">Deployment</a>
  </p>
</p>

---

## Why Chronos

Agentic software is fundamentally different from traditional request–response systems. Agents reason, call tools, pause for human approval, and resume later. They collaborate with other agents, maintain memory across sessions, and make decisions under uncertainty.

Chronos provides the full stack for building this kind of software in Go:

| Layer | Responsibility |
|-------|---------------|
| **SDK** | Agent builder, skills, memory, knowledge, teams, inter-agent protocol |
| **Engine** | StateGraph runtime, model providers, tool registry, guardrails, streaming |
| **ChronosOS** | HTTP control plane, auth, tracing, audit logs, approval enforcement |
| **Storage** | Pluggable persistence for sessions, checkpoints, memory, vectors |

---

## Features

- **YAML-First Configuration** — Define agents, models, and storage in `.chronos/agents.yaml` with `${ENV_VAR}` expansion and defaults inheritance
- **Multi-Provider LLM Support** — OpenAI, Anthropic, Google Gemini, Mistral, Ollama, Azure OpenAI, and any OpenAI-compatible endpoint (Together, Groq, DeepSeek, OpenRouter, Fireworks, and more)
- **Embedding Providers** — OpenAI and Ollama embeddings for RAG pipelines, with a caching layer
- **Agent Communication Protocol** — Agents delegate tasks, share results, ask questions, broadcast updates, and hand off conversations via a typed message bus
- **Durable Execution** — StateGraph runtime with checkpointing, interrupt nodes, and resume-from-checkpoint
- **Function Calling** — Automatic tool-call loop with before/after hooks on every tool and model call
- **Multi-Agent Teams** — Sequential, parallel, router, and coordinator strategies with shared context
- **Human-in-the-Loop** — Interrupt nodes and approval API for high-risk tool calls
- **Pluggable Storage** — Single interface with adapters for SQLite, PostgreSQL, Redis, MongoDB, DynamoDB
- **Vector Stores** — Qdrant, Pinecone, Weaviate, Milvus, Redis Vector adapters
- **Guardrails** — Input and output validation with blocklist, max-length, and custom guardrail support
- **Knowledge (RAG)** — Vector-backed retrieval automatically injected into agent context
- **Memory** — Short-term and long-term memory with LLM-powered extraction, injected into every conversation
- **Observability** — Full tracing of node transitions, tool calls, and model responses; SSE streaming
- **CLI** — Interactive REPL with agent chat, shell escape, slash commands, session/memory management, and headless batch mode
- **Sandbox** — Process-level and Docker container isolation with resource limits
- **Production Ready** — Docker, Helm chart (with HPA, Ingress, Secrets), horizontal scaling

---

## Quickstart

### Install

```bash
go get github.com/spawn08/chronos
```

### Option A: YAML Config (recommended)

Create `.chronos/agents.yaml` in your project:

```yaml
defaults:
  model:
    provider: openai
    api_key: ${OPENAI_API_KEY}
  storage:
    backend: sqlite
    dsn: chronos.db

agents:
  - id: dev
    name: Dev Agent
    model:
      model: gpt-4o
    system_prompt: You are a senior software engineer. Be concise.
```

Then use the CLI:

```bash
# Chat interactively
go run ./cli/main.go repl

# One-shot query
go run ./cli/main.go run "explain Go interfaces"

# List configured agents
go run ./cli/main.go agent list
```

Or load from Go code:

```go
fc, _ := agent.LoadFile("")  // auto-discovers .chronos/agents.yaml
cfg, _ := fc.FindAgent("dev")
a, _ := agent.BuildAgent(ctx, cfg)
resp, _ := a.Chat(ctx, "What is the capital of France?")
```

### Option B: Go Builder API

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
)

func main() {
    a, _ := agent.New("chat-agent", "Chat Agent").
        WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
        WithSystemPrompt("You are a helpful assistant.").
        Build()

    resp, _ := a.Chat(context.Background(), "What is the capital of France?")
    fmt.Println(resp.Content)
}
```

### Graph-Based Agent

```go
store, _ := sqlite.New("myagent.db")
defer store.Close()
store.Migrate(ctx)

g := graph.New("hello").
    AddNode("greet", func(_ context.Context, s graph.State) (graph.State, error) {
        s["message"] = fmt.Sprintf("Hello, %s!", s["user"])
        return s, nil
    }).
    SetEntryPoint("greet").
    SetFinishPoint("greet")

a, _ := agent.New("hello-agent", "Hello Agent").
    WithStorage(store).
    WithGraph(g).
    Build()

result, _ := a.Run(ctx, map[string]any{"user": "World"})
fmt.Println(result.State["message"]) // Hello, World!
```

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                   ChronosOS  (Control Plane)                 │
│   Auth & RBAC  │  Tracing & Audit  │  Approval  │  HTTP API │
└────────────────────────────┬─────────────────────────────────┘
                             │
┌────────────────────────────▼─────────────────────────────────┐
│                         Engine                               │
│  StateGraph Runtime │ Model Providers │ Tools │ Guardrails   │
│  Hooks & Middleware │ SSE Streaming                          │
└────────────────────────────┬─────────────────────────────────┘
                             │
┌────────────────────────────▼─────────────────────────────────┐
│                          SDK                                 │
│  Agent Builder │ Teams │ Protocol Bus │ Skills │ Memory/RAG  │
└────────────────────────────┬─────────────────────────────────┘
                             │
┌────────────────────────────▼─────────────────────────────────┐
│                    Storage  (Pluggable)                       │
│  SQLite │ PostgreSQL │ Redis │ MongoDB │ DynamoDB            │
│  Qdrant │ Pinecone │ Weaviate │ Milvus                      │
└──────────────────────────────────────────────────────────────┘
```

### Project Structure

```
chronos/
├── engine/                    # Runtime layer
│   ├── graph/                 # StateGraph: nodes, edges, checkpoints, resume
│   ├── model/                 # LLM provider implementations
│   │   ├── provider.go        # Provider interface and shared types
│   │   ├── openai.go          # OpenAI (GPT-4o, o1, o3)
│   │   ├── anthropic.go       # Anthropic (Claude)
│   │   ├── gemini.go          # Google Gemini
│   │   ├── mistral.go         # Mistral AI
│   │   ├── ollama.go          # Ollama (local models)
│   │   ├── azure.go           # Azure OpenAI
│   │   ├── compatible.go      # Any OpenAI-compatible API
│   │   ├── openai_embeddings.go  # OpenAI embeddings provider
│   │   ├── ollama_embeddings.go  # Ollama embeddings provider
│   │   ├── embeddings.go      # EmbeddingsProvider interface + caching
│   │   └── httpclient.go      # Shared HTTP transport
│   ├── tool/                  # Tool registry with permissions
│   ├── guardrails/            # Input/output validation
│   ├── hooks/                 # Before/after middleware chain
│   └── stream/                # SSE event broker
├── sdk/                       # User-facing API layer
│   ├── agent/
│   │   ├── agent.go           # Agent definition and fluent builder
│   │   └── config.go          # YAML config parser and agent loader
│   ├── team/                  # Multi-agent orchestration strategies
│   ├── protocol/              # Agent-to-agent communication bus
│   ├── skill/                 # Skill metadata and registry
│   ├── memory/                # Short/long-term memory + LLM manager
│   └── knowledge/             # RAG: Knowledge interface, VectorKnowledge
├── storage/                   # Persistence layer
│   ├── storage.go             # Storage interface (18 methods)
│   ├── vector.go              # VectorStore interface
│   └── adapters/
│       ├── sqlite/            # SQLite (dev/test)
│       ├── postgres/          # PostgreSQL (production)
│       ├── qdrant/            # Qdrant vector store
│       ├── redis/             # Redis key-value storage
│       ├── redisvector/       # Redis vector store (RediSearch)
│       ├── mongo/             # MongoDB document storage
│       ├── dynamo/            # DynamoDB serverless storage
│       ├── pinecone/          # Pinecone vector store
│       ├── weaviate/          # Weaviate vector store
│       └── milvus/            # Milvus vector store
├── os/                        # ChronosOS control plane
│   ├── server.go              # HTTP API server
│   ├── auth/                  # RBAC and authentication
│   ├── trace/                 # Span collector and audit logging
│   └── approval/              # Human-in-the-loop approval service
├── cli/                       # Command-line interface
│   ├── main.go                # Entry point
│   ├── cmd/                   # CLI commands (repl, serve, run, agent, sessions, memory, db, config)
│   └── repl/                  # Interactive REPL with agent chat and slash commands
├── sandbox/
│   ├── sandbox.go             # Sandbox interface + ProcessSandbox
│   └── container.go           # ContainerSandbox (Docker API)
├── .chronos/
│   └── agents.yaml            # Example agent configuration
├── examples/
├── deploy/
│   ├── docker/
│   └── helm/chronos/          # Helm chart (Deployment, Service, Secret, Ingress, HPA, ServiceAccount)
├── go.mod
└── README.md
```

---

## Agent Configuration (YAML)

Chronos agents can be defined entirely in YAML — no Go code required for basic setups. The CLI auto-discovers config files in this order:

1. `.chronos/agents.yaml` (project-level)
2. `agents.yaml` (current directory)
3. `~/.chronos/agents.yaml` (global)

Override with `CHRONOS_CONFIG=/path/to/config.yaml`.

```yaml
defaults:
  model:
    provider: openai
    api_key: ${OPENAI_API_KEY}      # Environment variable expansion
  storage:
    backend: sqlite
    dsn: chronos.db

agents:
  - id: dev
    name: Dev Agent
    description: General-purpose coding assistant
    model:
      model: gpt-4o
    system_prompt: |
      You are a senior software engineer. Be concise.
    instructions:
      - Always use context.Context as the first parameter.
    capabilities: [code, review]

  - id: researcher
    name: Research Agent
    model:
      provider: anthropic                # Override the default provider
      model: claude-sonnet-4-6
      api_key: ${ANTHROPIC_API_KEY}
    system_prompt: You are a research analyst.

  - id: local
    name: Local Agent
    model:
      provider: ollama
      model: llama3.3
    storage:
      backend: sqlite
      dsn: local.db

  - id: coordinator
    name: Team Lead
    model:
      model: gpt-4o
    sub_agents: [dev, researcher]        # Wires sub-agent references
```

### Supported Providers in YAML

`openai`, `anthropic`, `gemini`, `mistral`, `ollama`, `azure`, `groq`, `together`, `deepseek`, `openrouter`, `fireworks`, `perplexity`, `anyscale`, `compatible`

### Loading from Go

```go
fc, _ := agent.LoadFile("")                    // auto-discover
cfg, _ := fc.FindAgent("dev")                  // lookup by ID or name
a, _ := agent.BuildAgent(ctx, cfg)             // fully wired agent

agents, _ := agent.BuildAll(ctx, fc)           // build all, wire sub-agents
coordinator := agents["coordinator"]
```

---

## Model Providers

Chronos supports every major LLM provider through a single `Provider` interface. Swap providers with one line — no code changes needed.

### Supported Providers

| Provider | Constructor | Models |
|----------|------------|--------|
| **OpenAI** | `model.NewOpenAI(key)` | GPT-4o, GPT-4, GPT-3.5-turbo, o1, o3 |
| **Anthropic** | `model.NewAnthropic(key)` | Claude Sonnet, Opus, Haiku |
| **Google Gemini** | `model.NewGemini(key)` | Gemini 2.0 Flash, Gemini Pro |
| **Mistral** | `model.NewMistral(key)` | Mistral Large, Medium, Small |
| **Ollama** | `model.NewOllama(host, model)` | Llama 3, Mistral, CodeLlama, any local model |
| **Azure OpenAI** | `model.NewAzureOpenAI(endpoint, key, deployment)` | Any Azure-deployed model |
| **OpenAI-Compatible** | `model.NewOpenAICompatible(name, url, key, model)` | vLLM, TGI, LiteLLM, any custom server |

### Convenience Constructors

```go
model.NewTogether(key, model)      // Together AI
model.NewGroq(key, model)          // Groq
model.NewDeepSeek(key, model)      // DeepSeek
model.NewOpenRouter(key, model)    // OpenRouter
model.NewFireworks(key, model)     // Fireworks AI
model.NewPerplexity(key, model)    // Perplexity
model.NewAnyscale(key, model)      // Anyscale Endpoints
```

### Usage

```go
// OpenAI
a, _ := agent.New("a1", "Agent").
    WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
    Build()

// Anthropic
a, _ := agent.New("a2", "Agent").
    WithModel(model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))).
    Build()

// Local Ollama
a, _ := agent.New("a3", "Agent").
    WithModel(model.NewOllama("http://localhost:11434", "llama3.2")).
    Build()

// Custom endpoint (e.g., self-hosted vLLM)
a, _ := agent.New("a4", "Agent").
    WithModel(model.NewOpenAICompatible("vllm", "http://gpu-server:8000/v1", "", "meta-llama/Llama-3.1-70B")).
    Build()

// Full configuration
a, _ := agent.New("a5", "Agent").
    WithModel(model.NewOpenAIWithConfig(model.ProviderConfig{
        APIKey:     os.Getenv("OPENAI_API_KEY"),
        Model:      "gpt-4o",
        MaxRetries: 3,
        TimeoutSec: 60,
    })).
    Build()
```

### Embedding Providers

Used for RAG pipelines and knowledge base search.

```go
// OpenAI embeddings
emb := model.NewOpenAIEmbeddings(os.Getenv("OPENAI_API_KEY"))

// Ollama local embeddings
emb := model.NewOllamaEmbeddings("http://localhost:11434", "nomic-embed-text")

// With caching
emb := model.NewCachedEmbeddings(model.NewOpenAIEmbeddings(key))

resp, _ := emb.Embed(ctx, &model.EmbeddingRequest{
    Input: []string{"Hello world", "Goodbye world"},
})
// resp.Embeddings contains [][]float32
```

### Streaming

Every provider supports streaming via `StreamChat`:

```go
ch, _ := provider.StreamChat(ctx, &model.ChatRequest{
    Messages: []model.Message{{Role: "user", Content: "Tell me a story"}},
})
for chunk := range ch {
    fmt.Print(chunk.Content) // prints tokens as they arrive
}
```

---

## Multi-Agent Protocol

Chronos agents communicate like human developers on a team. The `protocol.Bus` routes typed messages between registered agents, enabling task delegation, result sharing, questions, and handoffs.

### How It Works

```
┌──────────┐     TaskRequest     ┌──────────┐
│ Architect ├───────────────────►│ Developer │
│          │◄───────────────────┤          │
└──────────┘     TaskResult      └──────────┘
      │                                │
      │  Broadcast("plan ready")       │  Broadcast("code ready")
      ▼                                ▼
┌──────────────────────────────────────────┐
│              Protocol Bus                │
│  Message routing, history, observability │
└──────────────────────────────────────────┘
```

### Message Types

| Type | Purpose |
|------|---------|
| `task_request` | Ask another agent to perform work |
| `task_result` | Return the outcome of a delegated task |
| `question` | Ask another agent for information |
| `answer` | Respond to a question |
| `broadcast` | Send an update to all agents |
| `handoff` | Transfer full ownership of a conversation |
| `status` | Report progress on a long-running task |
| `ack` | Acknowledge receipt |
| `error` | Signal a failure |

### Team Strategies

```go
// Sequential: architect → developer → reviewer (state flows through)
team.New("dev-team", "Dev Team", team.StrategySequential).
    AddAgent(architect).
    AddAgent(developer).
    AddAgent(reviewer)

// Parallel: all agents run concurrently, results merged
team.New("research", "Research Team", team.StrategyParallel).
    AddAgent(webSearcher).
    AddAgent(dbAnalyst).
    SetMerge(combineResults)

// Router: a function selects which agent handles the input
team.New("support", "Support", team.StrategyRouter).
    AddAgent(billing).
    AddAgent(technical).
    SetRouter(routeByIntent)

// Coordinator: first agent decomposes tasks, delegates to specialists via the bus
team.New("project", "Project Team", team.StrategyCoordinator).
    AddAgent(projectManager). // coordinator
    AddAgent(frontendDev).
    AddAgent(backendDev)
```

### Direct Communication

```go
bus := protocol.NewBus()

// Register agents
bus.Register("arch", "Architect", "Plans systems", []string{"architecture"}, handler)
bus.Register("dev", "Developer", "Writes code", []string{"coding"}, handler)

// Delegate a task and wait for the result
result, _ := bus.DelegateTask(ctx, "arch", "dev", "Implement auth",
    protocol.TaskPayload{
        Description: "Implement JWT authentication",
        Input:       map[string]any{"spec": authSpec},
    })
fmt.Println(result.Output)

// Ask a question
answer, _ := bus.Ask(ctx, "dev", "arch", "Should we use RS256 or HS256?")

// Find agents by capability
coders := bus.FindByCapability("coding")
```

---

## StateGraph Runtime

The engine provides a durable graph runtime where each node is a function that transforms state. Execution is checkpointed after every node, enabling pause/resume and time-travel debugging.

```go
g := graph.New("pipeline").
    AddNode("extract", extractFn).
    AddNode("transform", transformFn).
    AddNode("validate", validateFn).
    AddInterruptNode("approve", approveFn). // pauses for human approval
    AddNode("load", loadFn).
    SetEntryPoint("extract").
    AddEdge("extract", "transform").
    AddEdge("transform", "validate").
    AddConditionalEdge("validate", func(s graph.State) string {
        if s["valid"].(bool) {
            return "approve"
        }
        return "transform" // retry
    }).
    AddEdge("approve", "load").
    SetFinishPoint("load")
```

### Features

- **Checkpointing** — State saved after every node for crash recovery
- **Interrupt Nodes** — Pause execution for human-in-the-loop approval
- **Conditional Edges** — Dynamic routing based on state
- **Resume** — Continue from the latest checkpoint or any historical checkpoint
- **Stream Events** — Real-time `node_start`, `node_end`, `edge_transition`, `interrupt`, `error`, `completed` events

---

## Tools & Function Calling

Register tools that the LLM can call. Chronos handles the full tool-call loop automatically.

```go
a, _ := agent.New("assistant", "Assistant").
    WithModel(model.NewOpenAI(key)).
    WithSystemPrompt("You are a helpful assistant with access to tools.").
    AddTool(&tool.Definition{
        Name:        "get_weather",
        Description: "Get current weather for a city",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "city": map[string]string{"type": "string", "description": "City name"},
            },
            "required": []string{"city"},
        },
        Permission: tool.PermAllow,
        Handler: func(ctx context.Context, args map[string]any) (any, error) {
            return map[string]string{"temp": "22°C", "city": args["city"].(string)}, nil
        },
    }).
    Build()

// The model will automatically call get_weather if relevant
resp, _ := a.Chat(ctx, "What's the weather in Tokyo?")
```

### Permissions

| Level | Behavior |
|-------|----------|
| `PermAllow` | Executed immediately |
| `PermRequireApproval` | Paused until a human approves via the approval API |
| `PermDeny` | Blocked — the model is told the tool is unavailable |

---

## Storage

All storage adapters implement the same `Storage` interface (18 methods covering sessions, memory, audit logs, traces, events, and checkpoints). Swap backends with zero code changes.

```go
// Development
store, _ := sqlite.New("dev.db")

// Production
store, _ := postgres.New("postgres://user:pass@host:5432/chronos")

// Vector store for RAG
vectors := qdrant.New("http://localhost:6333")
vectors.CreateCollection(ctx, "docs", 1536)
```

| Adapter | Type | Status |
|---------|------|--------|
| SQLite | Storage | Production-ready |
| PostgreSQL | Storage | Production-ready |
| Qdrant | VectorStore | Production-ready |
| Redis | Storage | Available |
| Redis Vector | VectorStore | Available (RediSearch) |
| MongoDB | Storage | Available |
| DynamoDB | Storage | Available |
| Pinecone | VectorStore | Available |
| Weaviate | VectorStore | Available |
| Milvus | VectorStore | Available |

---

## Guardrails

Validate inputs and outputs before they reach the model or the user.

```go
agent.New("safe", "Safe Agent").
    AddInputGuardrail("blocklist", &guardrails.BlocklistGuardrail{
        Words: []string{"hack", "exploit"},
    }).
    AddOutputGuardrail("max_length", &guardrails.MaxLengthGuardrail{
        MaxLength: 4096,
    })
```

---

## CLI

The CLI auto-loads agents from `.chronos/agents.yaml` when available.

```bash
# Interactive REPL (loads default agent from config)
chronos repl

# Chat with a specific agent
chronos agent chat dev

# One-shot message (headless)
chronos run "explain Go interfaces"
chronos run --agent researcher "compare React vs Svelte"

# Agent management
chronos agent list                   # List all configured agents
chronos agent show dev               # Show agent details

# Session management
chronos sessions list                # List past sessions
chronos sessions export <id>        # Export session as markdown

# Memory, storage, config
chronos memory list <agent_id>       # Show stored memories
chronos db init                      # Initialize database
chronos db status                    # Show database info
chronos config show                  # Show config and loaded agents

# Control plane server
chronos serve :8420
```

### REPL Commands

```
dev> /help           Show available commands
dev> /agent          Show current agent info
dev> /model          Show current model info
dev> /sessions       List recent sessions
dev> /memory         List memories for current agent
dev> /history        Show conversation history
dev> /clear          Clear conversation history
dev> /quit           Exit
dev> ! ls -la        Run a shell command
```

Non-command input is sent directly to the loaded agent for chat.

---

## Examples

| Example | Description | Run |
|---------|-------------|-----|
| [quickstart](examples/quickstart/) | Minimal agent with SQLite and a 3-node graph | `go run ./examples/quickstart/main.go` |
| [multi_provider](examples/multi_provider/) | Connect to OpenAI, Anthropic, Gemini, Mistral, Ollama | `go run ./examples/multi_provider/main.go` |
| [multi_agent](examples/multi_agent/) | Team of agents communicating via protocol bus | `go run ./examples/multi_agent/main.go` |

---

## Deployment

### Docker

```bash
docker build -f deploy/docker/Dockerfile -t chronos .
docker run -p 8420:8420 chronos
```

### Kubernetes (Helm)

The Helm chart includes Deployment, Service, Secret, Ingress, HPA, and ServiceAccount templates.

```bash
helm install chronos deploy/helm/chronos/ \
  --set image.tag=latest \
  --set secrets.storageDSN="postgres://user:pass@db:5432/chronos" \
  --set ingress.enabled=true \
  --set autoscaling.enabled=true
```

### Container Sandbox

For isolated execution of untrusted code, Chronos provides a Docker-based sandbox with resource limits:

```go
sandbox := sandbox.NewContainerSandbox(sandbox.ContainerConfig{
    Image:       "python:3.12-slim",
    MemoryBytes: 256 * 1024 * 1024,  // 256 MiB
    CPUQuota:    50000,               // 50% of one core
    NetworkMode: "none",              // no network access
})

result, _ := sandbox.Execute(ctx, "python", []string{"-c", "print('hello')"}, 30*time.Second)
fmt.Println(result.Stdout) // hello
```

---

## Build from Source

```bash
git clone https://github.com/spawn08/chronos.git
cd chronos
```

Use `make` for all common tasks:

```bash
make build          # Compile the CLI binary to bin/chronos
make build-all      # Compile every package (including examples)
make test           # Run all tests with race detector
make test-cover     # Tests + HTML coverage report
make lint           # Run golangci-lint
make fmt            # Format all source files
make vet            # Run go vet
make tidy           # go mod tidy + verify
make all            # fmt + vet + lint + build (default CI pipeline)
make docker-build   # Build the Docker image
make build-cross    # Cross-compile for linux/darwin amd64/arm64
make clean          # Remove build artifacts
make help           # Show all available targets
```

Or use `go` directly:

```bash
go build ./...
go vet ./...
go test -race ./...
go run ./examples/quickstart/main.go
```

**Requirements:** Go 1.24+ and a C compiler (for SQLite via CGO).

---

## CI/CD

The project includes GitHub Actions workflows for continuous integration and releases.

### CI (`.github/workflows/ci.yml`)

Runs on every push to `main` and on pull requests:

- **Lint** — `golangci-lint` with the project `.golangci.yml` config
- **Build & Test** — compiles all packages, runs tests with race detector on Ubuntu and macOS
- **Examples** — smoke-tests all example programs
- **Docker** — verifies the Docker image builds successfully

### Release (`.github/workflows/release.yml`)

Triggered when a semver tag (`v*.*.*`) is pushed:

1. **Tests** gate the release
2. **Go module** is published to the Go module proxy
3. **Cross-platform binaries** are built for Linux, macOS, and Windows (amd64 + arm64)
4. **GitHub Release** is created with binaries and SHA-256 checksums
5. **Docker image** is built for `linux/amd64` and `linux/arm64` and pushed to GitHub Container Registry (`ghcr.io`)

To cut a release:

```bash
git tag v0.2.0
git push origin v0.2.0
```

### Dependabot

Automated dependency updates are configured for Go modules, GitHub Actions, and Docker base images.

---

## Key Interfaces

When extending Chronos, implement these interfaces:

```go
// LLM provider
type Provider interface {
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error)
    Name() string
    Model() string
}

// Embedding provider for RAG
type EmbeddingsProvider interface {
    Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
}

// Persistent storage (18 methods total)
type Storage interface {
    CreateSession(ctx, *Session) error
    GetSession(ctx, id) (*Session, error)
    SaveCheckpoint(ctx, *Checkpoint) error
    // ... sessions, memory, audit logs, traces, events, checkpoints
    Migrate(ctx) error
    Close() error
}

// Vector store for RAG
type VectorStore interface {
    Upsert(ctx, collection string, embeddings []Embedding) error
    Search(ctx, collection string, query []float32, topK int) ([]SearchResult, error)
    Delete(ctx, collection string, ids []string) error
    CreateCollection(ctx, name string, dimension int) error
    Close() error
}

// Guardrail for input/output validation
type Guardrail interface {
    Check(ctx context.Context, content string) *Result
}

// Sandbox for isolated execution
type Sandbox interface {
    Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error)
    Close() error
}
```

---

## Contributing

We welcome contributions. Please follow these guidelines:

1. **Fork and branch** — create a feature branch from `main`
2. **Follow Go conventions** — `go vet`, `gofmt`, meaningful error wrapping
3. **No `init()` functions** — use explicit constructors
4. **No global state** — pass dependencies via constructors
5. **Wrap errors** — always `fmt.Errorf("context: %w", err)`
6. **JSON tags** — on all exported struct fields
7. **`context.Context` first** — on all I/O methods
8. **Tests** — table-driven tests in `*_test.go` files

---

## License

[Apache 2.0](LICENSE)
