---
name: chronos-quickstart
description: Get started with the Chronos Go AI agents framework — add dependency, create your first agent, and run it. Use when a developer is new to Chronos or starting a new project.
---

# Chronos Quickstart

## Activation
Use this skill when:
- Setting up Chronos in a new Go project for the first time
- Developer asks "how do I get started with Chronos"
- Creating a minimal working agent from scratch
- Bootstrapping a project that will use Chronos agents

## About Chronos
Chronos (`github.com/spawn08/chronos`) is a Go framework for building durable, scalable AI agents. It provides:
- Fluent builder API for agent construction
- YAML-based agent/team definitions with CLI runner
- StateGraph workflows with checkpointing and resume
- 14+ model providers (OpenAI, Anthropic, Gemini, Mistral, Ollama, Azure, Groq, etc.)
- Multi-agent teams (sequential, parallel, router, coordinator, swarm, hierarchy)
- RAG/knowledge pipelines with vector stores
- Memory (short-term + long-term with vector recall)
- MCP server integration
- Streaming (SSE), hooks, guardrails, sandboxing
- ChronosOS HTTP control plane with auth, tracing, monitoring

## Step 1: Add the Dependency

```bash
go get github.com/spawn08/chronos
```

## Step 2: Choose Your Approach

### Option A: Go Code (Programmatic)

Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()

    // 1. Storage (SQLite for dev, Postgres for prod)
    store, err := sqlite.New("agent.db")
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()
    if err := store.Migrate(ctx); err != nil {
        log.Fatal(err)
    }

    // 2. Model provider
    llm := model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))

    // 3. Build agent
    a, err := agent.New("assistant", "My Assistant").
        Description("A helpful AI assistant").
        WithModel(llm).
        WithStorage(store).
        WithSystemPrompt("You are a helpful assistant. Be concise.").
        WithStreaming(true).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    // 4. Chat
    resp, err := a.Chat(ctx, "What is Chronos?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Content)
}
```

Run:
```bash
export ANTHROPIC_API_KEY="sk-ant-..."
go run main.go
```

### Option B: YAML Config (Declarative)

Create `agents.yaml`:

```yaml
defaults:
  model:
    provider: anthropic
    model: claude-sonnet-4-20250514
    api_key: ${ANTHROPIC_API_KEY}
  storage:
    backend: sqlite
    dsn: "agent.db"

agents:
  - id: "assistant"
    name: "My Assistant"
    description: "A helpful AI assistant"
    system_prompt: "You are a helpful assistant. Be concise."
    stream: true
```

Run with the Chronos CLI:
```bash
# Install CLI
go install github.com/spawn08/chronos/cli@latest

# Run once
chronos run -c agents.yaml -a assistant -m "What is Chronos?"

# Interactive REPL
chronos agent chat -c agents.yaml -a assistant

# Serve as HTTP API
chronos serve :8420
```

Or load YAML in your Go code:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/spawn08/chronos/sdk/agent"
)

func main() {
    ctx := context.Background()

    cfg, err := agent.LoadFile("agents.yaml")
    if err != nil {
        log.Fatal(err)
    }

    agents, err := agent.BuildAll(ctx, cfg)
    if err != nil {
        log.Fatal(err)
    }

    a := agents["assistant"]
    resp, err := a.Chat(ctx, "Hello!")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Content)
}
```

## Step 3: Verify

```bash
go build ./...
go run main.go
```

## Available Model Providers

| Provider | Constructor | Env Var |
|----------|-------------|---------|
| OpenAI | `model.NewOpenAI(apiKey)` | `OPENAI_API_KEY` |
| Anthropic | `model.NewAnthropic(apiKey)` | `ANTHROPIC_API_KEY` |
| Gemini | `model.NewGemini(apiKey)` | `GEMINI_API_KEY` |
| Mistral | `model.NewMistral(apiKey)` | `MISTRAL_API_KEY` |
| Ollama | `model.NewOllama(host, modelName)` | — (local) |
| Azure | `model.NewAzureOpenAI(endpoint, apiKey, deployment)` | `AZURE_OPENAI_API_KEY` |
| Groq | `model.NewGroq(apiKey, modelID)` | `GROQ_API_KEY` |
| Together | `model.NewTogether(apiKey, modelID)` | `TOGETHER_API_KEY` |
| DeepSeek | `model.NewDeepSeek(apiKey, modelID)` | `DEEPSEEK_API_KEY` |
| OpenRouter | `model.NewOpenRouter(apiKey, modelID)` | `OPENROUTER_API_KEY` |
| Fireworks | `model.NewFireworks(apiKey, modelID)` | `FIREWORKS_API_KEY` |
| Perplexity | `model.NewPerplexity(apiKey, modelID)` | `PERPLEXITY_API_KEY` |
| Anyscale | `model.NewAnyscale(apiKey, modelID)` | `ANYSCALE_API_KEY` |
| Compatible | `model.NewOpenAICompatible(name, baseURL, apiKey, modelID)` | — |

YAML `provider` values: `openai`, `anthropic`, `gemini`, `mistral`, `ollama`, `azure`, `groq`, `together`, `deepseek`, `openrouter`, `fireworks`, `perplexity`, `anyscale`, `compatible`

## Available Storage Backends

| Backend | Constructor | YAML `backend` |
|---------|-------------|----------------|
| SQLite (dev) | `sqlite.New(dsn)` | `sqlite` |
| PostgreSQL (prod) | `postgres.New(dsn)` | `postgres` |
| In-memory | `memory.New()` | — |
| Redis | `redis.New(addr, password, db)` | — |
| MongoDB | `mongo.New(uri, database)` | — |
| DynamoDB | `dynamo.New(endpoint, tableName, region, accessKey, secretKey)` | — |

## Next Steps
- Add tools → use the `chronos-tools` skill
- Build a StateGraph workflow → use the `chronos-graph` skill
- Set up RAG → use the `chronos-rag` skill
- Add memory → use the `chronos-memory` skill
- Create multi-agent teams → use the `chronos-teams` skill
- Deploy to production → use the `chronos-deploy` skill
