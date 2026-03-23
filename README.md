<p align="center">
  <h1 align="center">Chronos</h1>
  <p align="center">
    A Go framework for building durable, scalable AI agents.<br />
    Define agents. Connect any LLM. Let them collaborate.
  </p>
  <p align="center">
    <a href="#install">Install</a> &middot;
    <a href="#quickstart">Quickstart</a> &middot;
    <a href="https://spawn08.github.io/chronos/">Docs</a> &middot;
    <a href="#examples">Examples</a> &middot;
    <a href="ROADMAP.md">Roadmap</a>
  </p>
</p>

---

## Features

| Layer | What it does |
|-------|-------------|
| **SDK** | Agent builder, teams (sequential/parallel/router/coordinator), memory, knowledge (RAG), inter-agent protocol bus |
| **Engine** | StateGraph runtime with checkpointing and interrupt nodes, 14+ LLM providers, tool registry, guardrails, hooks, SSE streaming |
| **ChronosOS** | HTTP control plane — auth, RBAC, tracing, audit logs, approval API |
| **Storage** | SQLite, PostgreSQL, Redis, MongoDB, DynamoDB, Qdrant, Pinecone, Weaviate, Milvus |
| **CLI** | Interactive REPL, headless batch mode, session/memory management, YAML-first config |

---

## Install

### CLI Binary (Linux / macOS / Windows)

```bash
curl -fsSL https://raw.githubusercontent.com/spawn08/chronos/main/install.sh | bash
```

Pre-built binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64` (Intel), `darwin/arm64` (Apple Silicon), `windows/amd64`, and `windows/arm64` are published to [GitHub Releases](https://github.com/spawn08/chronos/releases).

### Go Module

```bash
go get github.com/spawn08/chronos
```

### Build from Source

```bash
git clone https://github.com/spawn08/chronos.git && cd chronos
make build    # outputs bin/chronos
```

---

## Quickstart

**YAML config** — create `.chronos/agents.yaml`:

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
    system_prompt: You are a senior software engineer.
```

```bash
chronos repl                            # interactive chat
chronos run "explain Go interfaces"     # headless one-shot
```

**Go builder API:**

```go
a, _ := agent.New("chat", "Chat Agent").
    WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
    WithSystemPrompt("You are a helpful assistant.").
    Build()

resp, _ := a.Chat(ctx, "What is the capital of France?")
fmt.Println(resp.Content)
```

**Graph-based agent:**

```go
g := graph.New("pipeline").
    AddNode("greet", func(_ context.Context, s graph.State) (graph.State, error) {
        s["message"] = fmt.Sprintf("Hello, %s!", s["user"])
        return s, nil
    }).
    SetEntryPoint("greet").
    SetFinishPoint("greet")

a, _ := agent.New("hello", "Hello Agent").WithGraph(g).Build()
result, _ := a.Run(ctx, map[string]any{"user": "World"})
```

---

## Examples

All examples with **No** API keys run with mock providers — no external calls.

| Example | Description | Needs Keys? |
|---------|-------------|:-----------:|
| [quickstart](examples/quickstart/) | Minimal agent with SQLite and 3-node graph | No |
| [tools_and_guardrails](examples/tools_and_guardrails/) | Tool permissions + input/output guardrails | No |
| [hooks_observability](examples/hooks_observability/) | Metrics, cost tracking, caching, retry, rate limiting | No |
| [graph_patterns](examples/graph_patterns/) | Conditional edges, interrupt nodes, checkpoints | No |
| [memory_and_sessions](examples/memory_and_sessions/) | Short/long-term memory, multi-turn sessions | No |
| [streaming_sse](examples/streaming_sse/) | Pub/sub broker, graph events, SSE HTTP server | No |
| [chat_with_tools](examples/chat_with_tools/) | Agent chat with calculator and lookup tools | No |
| [fallback_provider](examples/fallback_provider/) | Provider chain with automatic failover | No |
| [sandbox_execution](examples/sandbox_execution/) | Process sandbox with timeouts and I/O capture | No |
| [multi_agent](examples/multi_agent/) | All 4 team strategies, bus delegation | Optional |
| [multi_provider](examples/multi_provider/) | OpenAI, Anthropic, Gemini, Mistral, Ollama | Yes |

Run any example: `go run ./examples/<name>/`

---

## Supported Providers

OpenAI, Anthropic, Google Gemini, Mistral, Ollama, Azure OpenAI, and any OpenAI-compatible endpoint (Together, Groq, DeepSeek, OpenRouter, Fireworks, Perplexity, Anyscale, vLLM, LiteLLM).

---

## Documentation

Full docs at **[spawn08.github.io/chronos](https://spawn08.github.io/chronos/)**:

- [Installation](https://spawn08.github.io/chronos/getting-started/installation/) — CLI binary, Go module, build from source
- [CLI Install](https://spawn08.github.io/chronos/getting-started/cli-install/) — curl install for all platforms
- [Quickstart](https://spawn08.github.io/chronos/getting-started/quickstart/) — First agent in 5 minutes
- [Agents](https://spawn08.github.io/chronos/guides/agents/) — Agent builder, YAML config, capabilities
- [Teams](https://spawn08.github.io/chronos/guides/teams/) — Multi-agent orchestration strategies
- [StateGraph](https://spawn08.github.io/chronos/guides/stategraph/) — Durable execution with checkpointing
- [Tools](https://spawn08.github.io/chronos/guides/tools/) — Function calling and permissions
- [Hooks](https://spawn08.github.io/chronos/guides/hooks/) — Middleware: retry, cache, cost, rate limit
- [Storage](https://spawn08.github.io/chronos/guides/storage/) — All 10 storage and vector adapters

---

## Roadmap

Active development tracked in [ROADMAP.md](ROADMAP.md). Key upcoming work:

| Priority | Area | Status |
|----------|------|--------|
| **P0** | Bug fixes, CLI wiring, test foundation | 5/16 done |
| **P1** | MCP support, subgraphs, time travel, advanced HITL | Planned |
| **P2** | A2A protocol, cron triggers, multi-modal, graph visualization | Planned |
| **P3** | Workflow DSL, plugin marketplace, distributed execution | Planned |

### What's Next (P1 highlights)

- **MCP (Model Context Protocol)** — Connect to MCP servers, use MCP tools natively
- **Subgraphs** — Compose graphs as nodes, parallel fan-out/fan-in with state reducers
- **Time Travel** — Replay from any checkpoint, fork execution with modified state
- **Advanced Streaming** — Multiple stream modes (values, updates, debug), custom event emission
- **Structured Output** — Response models with automatic validation and retry
- **Agentic Loops** — ReAct, iterative refinement, self-correcting tool call patterns

---

## CI/CD

| Workflow | Trigger | What it does |
|----------|---------|-------------|
| **CI** | Push/PR to `main` | Lint, build, test (Ubuntu + macOS), example smoke tests, Docker build |
| **Release** | Tag `v*.*.*` | Test gate, build 6 platform binaries, create GitHub Release with checksums, publish Go module, push Docker image to GHCR |

Cut a release: `git tag v0.2.0 && git push origin v0.2.0`

---

## Contributing

1. Fork and create a feature branch from `main`
2. Follow Go conventions — `go vet`, `gofmt`, wrap errors with `%w`
3. No `init()` functions, no global state, `context.Context` first on I/O methods
4. Table-driven tests in `*_test.go` files

---

## License

[Apache 2.0](LICENSE)
