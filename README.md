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
    <a href="#roadmap">Roadmap</a>
  </p>
</p>

---

## Features

| Layer | What it does |
|-------|-------------|
| **SDK** | Agent builder, teams (sequential/parallel/router/coordinator/swarm/hierarchy), memory (short-term + semantic long-term recall), knowledge (RAG), inter-agent protocol bus, planning tool, virtual filesystem, context-isolated subagents, automatic context compaction |
| **Engine** | StateGraph runtime with checkpointing, interrupt nodes, subgraphs, and time-travel replay/fork; 15+ LLM providers; tool registry; guardrails; hooks (retry, cache, cost, rate-limit); SSE streaming; durable work queue |
| **ChronosOS** | HTTP control plane — auth (JWT/API key/OIDC), RBAC, tracing/OTLP export, audit logs, approval API, Prometheus metrics |
| **Protocol interop** | MCP client + server (stdio/SSE), A2A (Agent-to-Agent) client + server, AG-UI standard event stream |
| **Storage** | SQLite, PostgreSQL (+ pgvector), Redis (+ RedisVector), MongoDB, DynamoDB, Qdrant, Pinecone, Weaviate, Milvus, Chroma, LanceDB, in-memory |
| **CLI** | Interactive REPL, headless batch mode, session/memory management, eval capture/gate, YAML-first config |

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
    stream: true
    permission_mode: prompt             # prompt | auto_approve | deny
    reasoning:
      native: true
      effort: medium
      summary: false
```

```bash
chronos repl                                      # interactive streaming chat
chronos run --stream "explain Go interfaces"      # headless token streaming
chronos --permission-mode auto_approve repl       # trusted local session
# At an approval prompt, enter "a" to approve all for this CLI session.
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

All examples with **No** API keys run with mock providers or a scripted/offline path — no external calls.

### Core

| Example | Description | Needs Keys? |
|---------|-------------|:-----------:|
| [quickstart](examples/quickstart/) | Minimal agent with SQLite and 3-node graph | No |
| [tools_and_guardrails](examples/tools_and_guardrails/) | Tool permissions + input/output guardrails | No |
| [chat_with_tools](examples/chat_with_tools/) | Agent chat with calculator and lookup tools | No |
| [multi_round_tools](examples/multi_round_tools/) | Multi-round sequential tool calls retaining full context | No |
| [structured_output](examples/structured_output/) | Strict JSON output (`json_object`/`json_schema`) decoded into a typed struct | Yes |
| [hooks_observability](examples/hooks_observability/) | Metrics, cost tracking, caching, retry, rate limiting | No |
| [fallback_provider](examples/fallback_provider/) | Provider chain with automatic failover | No |

### Graph & durability

| Example | Description | Needs Keys? |
|---------|-------------|:-----------:|
| [graph_patterns](examples/graph_patterns/) | Conditional edges, interrupt nodes, checkpoints | No |
| [graph_with_llm](examples/graph_with_llm/) | StateGraph with real LLM calls inside nodes, LLM-driven conditional routing | Yes |
| [durable_llm_graph](examples/durable_llm_graph/) | Where LLM calls happen in a graph and how the runtime makes them durable | No |
| [durable_queue](examples/durable_queue/) | Durable work queue: leased workers, durable sleep, park/signal HITL, orphan recovery | No |
| [durable_hitl](examples/durable_hitl/) | Human-in-the-loop approval with checkpoint + resume | No |

### Memory, knowledge & agent harness

| Example | Description | Needs Keys? |
|---------|-------------|:-----------:|
| [memory_and_sessions](examples/memory_and_sessions/) | Short/long-term memory, multi-turn sessions | No |
| [semantic_recall](examples/semantic_recall/) | Automatic semantic long-term recall via a vector-indexed `memory.Manager` | No |
| [multitenant_memory](examples/multitenant_memory/) | Per-user long-term memory isolation on one agent | No |
| [rag_knowledge](examples/rag_knowledge/) | RAG with `knowledge.VectorKnowledge` — chunking, batching, top-k retrieval | No |
| [planning_agent](examples/planning_agent/) | Built-in planning ("todo") tool with a durably persisted plan | No |
| [vfs_agent](examples/vfs_agent/) | Virtual filesystem for context offloading (`fs_write`/`fs_read`) | No |
| [subagents](examples/subagents/) | Context-isolated delegation to a subagent via `spawn_subagent` | No |
| [context_compaction](examples/context_compaction/) | Automatic context compaction as a session nears the model's context window | No |
| [deep_agent](examples/deep_agent/) | Batteries-included harness preset: planning + VFS + subagents + compaction + recall | No |
| [skills](examples/skills/) | Registering, listing, resolving, and versioning skills with `skill.Registry` | No |
| [coding_agent](examples/coding_agent/) | Cursor/Aider-style coding agent: file tools, shell exec, semantic code search | Yes |

### Multi-agent & protocol interop

| Example | Description | Needs Keys? |
|---------|-------------|:-----------:|
| [multi_agent](examples/multi_agent/) | All team strategies (sequential/parallel/router/coordinator/swarm/hierarchy), bus delegation | Optional |
| [team_deploy](examples/team_deploy/) | Deploy multi-agent teams from YAML with sandbox isolation | Yes |
| [mcp_agent](examples/mcp_agent/) | Agent using MCP tools over stdio transport | Yes |
| [mcp_sse](examples/mcp_sse/) | MCP client over the HTTP+SSE transport (self-contained demo server) | No |
| [mcp_server](examples/mcp_server/) | Expose Chronos tools to any MCP host (Claude Desktop, an IDE, another agent framework) | No |
| [a2a_interop](examples/a2a_interop/) | Durable Agent-to-Agent (A2A) client + server, remote agent wrapped as a subagent tool | No |
| [agui_stream](examples/agui_stream/) | Chronos runs surfaced as a standard AG-UI SSE event stream | No |
| [eval_loop](examples/eval_loop/) | Eval-driven loop: capture a dataset from a run, score it, gate on regressions | No |

### Sandboxing

| Example | Description | Needs Keys? |
|---------|-------------|:-----------:|
| [sandbox_execution](examples/sandbox_execution/) | Process sandbox with timeouts and I/O capture | No |
| [wasm_sandbox](examples/wasm_sandbox/) | Run an untrusted WASI module in the wazero-backed sandbox | No |
| [k8s_sandbox](examples/k8s_sandbox/) | Run a command as a hardened one-shot Kubernetes Job | No\* |

### Providers & production

| Example | Description | Needs Keys? |
|---------|-------------|:-----------:|
| [multi_provider](examples/multi_provider/) | OpenAI, Anthropic, Gemini, Mistral, Ollama, Azure OpenAI, Vertex AI, Bedrock | Yes |
| [azure](examples/azure/) | Azure OpenAI (chat + streaming) with deployment/API-version config | Yes |
| [azure_tools](examples/azure_tools/) | Multi-round tool calling against an Azure OpenAI deployment | Yes |
| [azure_rag](examples/azure_rag/) | End-to-end RAG on Azure OpenAI (embeddings + grounded chat) | Yes |
| [vertex](examples/vertex/) | Google Cloud Vertex AI via OpenAI-compatible endpoint + gcloud token | Yes |
| [enterprise_sso](examples/enterprise_sso/) | ChronosOS behind OIDC/JWKS SSO (Okta, Azure AD, Google, Auth0) | Yes |
| [data_residency](examples/data_residency/) | Per-tenant storage routing (EU vs US) with a single logical agent | No |
| [multitenancy](examples/multitenancy/) | Storage-level tenant isolation via `storage.WithTenant` | No |
| [server_embedded](examples/server_embedded/) | Run the ChronosOS control plane as a library inside your own Go binary | No |
| [cli_agent](examples/cli_agent/) | Build, inspect, and run an agent from YAML via the CLI | No |
| [cli_ops](examples/cli_ops/) | Operate Chronos from the CLI: serve, monitor, db, sessions, pipe, deploy | No |
| [yaml-configs](examples/yaml-configs/) | Reference YAML: teams, providers, sandbox deploys, skills — not a runnable binary | — |

Run any example: `go run ./examples/<name>/`

\* `k8s_sandbox` needs a reachable Kubernetes cluster; with none configured it prints setup guidance and exits cleanly.

Every example that needs a real LLM (`coding_agent`, `graph_with_llm`, `structured_output`, `mcp_agent`, `multi_agent`, `team_deploy`, `multi_provider`) resolves its provider through `examples/internal/providers.Pick()`, so any of these env combos works interchangeably:

| Provider | Environment |
|----------|-------------|
| OpenAI | `OPENAI_API_KEY` |
| Anthropic | `ANTHROPIC_API_KEY` |
| Google Gemini (AI Studio) | `GEMINI_API_KEY` |
| Azure OpenAI | `AZURE_OPENAI_API_KEY` + `AZURE_OPENAI_ENDPOINT` + `AZURE_OPENAI_DEPLOYMENT` (+ optional `AZURE_OPENAI_API_VERSION`) |
| Google Cloud Vertex AI | `GOOGLE_CLOUD_PROJECT` + `GOOGLE_ACCESS_TOKEN` (+ optional `GOOGLE_CLOUD_LOCATION`, `VERTEX_MODEL`) |
| AWS Bedrock | `AWS_REGION` + `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` (+ optional `BEDROCK_MODEL_ID`) |
| Mistral | `MISTRAL_API_KEY` |
| Ollama (local) | `OLLAMA_HOST` (+ optional `OLLAMA_MODEL`) |
| Any OpenAI-compatible | `OPENAI_COMPATIBLE_BASE_URL` + `OPENAI_COMPATIBLE_MODEL` (+ optional `_API_KEY`, `_NAME`) — Together, Groq, DeepSeek, OpenRouter, Fireworks, Perplexity, Anyscale, vLLM, LiteLLM |

---

## Supported Providers

OpenAI, Anthropic, Google Gemini, Azure OpenAI, AWS Bedrock, Mistral, Cohere, Ollama, Google Cloud Vertex AI (via an OpenAI-compatible endpoint), and any other OpenAI-compatible endpoint (Together, Groq, DeepSeek, OpenRouter, Fireworks, Perplexity, Anyscale, vLLM, LiteLLM).

---

## Documentation

In-repo feature guides (with detailed run steps) live in **[docs/](docs/)**:

- [Architecture](https://spawn08.github.io/chronos/reference/architecture/) — illustrated layer stack, request/graph/queue flows, HITL, MCP transports, and interfaces (on the docs site)
- [Sandbox backends](docs/sandbox-backends.md) — Process, Container, WASM (WASI), and Kubernetes Job isolation
- [MCP transports](docs/mcp-transports.md) — connect to MCP servers over stdio and HTTP+SSE
- [Eval suites](docs/eval-suites.md) — declare and run evaluation suites from YAML or Go

Full docs at **[spawn08.github.io/chronos](https://spawn08.github.io/chronos/)**:

- [Installation](https://spawn08.github.io/chronos/getting-started/installation/) — CLI binary, Go module, build from source
- [CLI Install](https://spawn08.github.io/chronos/getting-started/cli-install/) — curl install for all platforms
- [Quickstart](https://spawn08.github.io/chronos/getting-started/quickstart/) — First agent in 5 minutes
- [Agents](https://spawn08.github.io/chronos/guides/agents/) — Agent builder, YAML config, capabilities
- [Teams](https://spawn08.github.io/chronos/guides/teams/) — Multi-agent orchestration strategies
- [StateGraph](https://spawn08.github.io/chronos/guides/stategraph/) — Durable execution with checkpointing
- [Tools](https://spawn08.github.io/chronos/guides/tools/) — Function calling and permissions
- [Hooks](https://spawn08.github.io/chronos/guides/hooks/) — Middleware: retry, cache, cost, rate limit
- [Storage](https://spawn08.github.io/chronos/guides/storage/) — All 14 storage and vector adapters

---

## Server & API

`chronos serve [addr]` (default `:8420`) starts **ChronosOS** — the control-plane
HTTP server. It exposes a REST API over sessions, checkpoints, traces, schedules,
and human-in-the-loop approvals, plus a live SSE event stream, Prometheus metrics,
health/readiness probes, and an interactive **Swagger UI**. It is hardened by
default (timeouts, body limits, panic recovery, CORS, rate limiting, graceful
shutdown) and stateless, so you can run many replicas behind a load balancer.

```bash
chronos serve :8420
open http://localhost:8420/swagger/     # interactive API explorer
```

Authentication is **opt-in** (`CHRONOS_AUTH=none|jwt|apikey`) with JWT
(HS256/RS256/JWKS/OIDC) or API keys and per-tenant isolation. Role enforcement is
opt-in too (`CHRONOS_RBAC=true` → `admin > user > viewer`), and the Swagger UI can
be disabled on hardened deployments with `CHRONOS_SWAGGER=false`.

- [ChronosOS Server](https://spawn08.github.io/chronos/guides/server/) — start, configure, and operate the control plane
- [REST API Reference](https://spawn08.github.io/chronos/api/rest-api/) — every endpoint with curl examples
- [Authentication & Authorization](https://spawn08.github.io/chronos/guides/authentication/) — JWT, API keys, RBAC, tenants
- **Swagger UI** at `/swagger/` · OpenAPI JSON at `/swagger/doc.json`

---

## Roadmap

The original seven-tier build-out (101 tracked items: correctness/safety, scale
foundations, hardening/platform) is **complete** — see [PLAN.md](PLAN.md) for the
full history and acceptance criteria. Forward-looking capability work, aimed at
parity with Google ADK, LangGraph, and DeepAgents while keeping Chronos
self-hostable and Go-native, is tracked live in [plan/STATUS.md](plan/STATUS.md).

| Wave | Focus | Status |
|------|-------|--------|
| **Wave 1** — Parity foundations | Planning tool, virtual filesystem, context-isolated subagents, automatic compaction, deep-agent preset, A2A, MCP server, AG-UI stream | Complete |
| **Wave 2** — Make it lovable | Eval-driven dev loop, semantic long-term recall, RAG scaling | Complete |
| **Wave 2** (remaining) | Visual studio / graph debugger, one-command deploy | Planned |
| **Wave 3** — Breadth & enterprise lead | Live bidirectional (audio/video) streaming, curated connectors + plugin SDK, per-tenant budget/quota policy, model allow-lists & data-residency/PII policy, compliance-grade audit & export | Planned |

See [`plan/README.md`](plan/README.md) for the strategic thesis and workstream
breakdown, and [`plan/STATUS.md`](plan/STATUS.md) for the live, item-by-item tracker.

---

## CI/CD

| Workflow | Trigger | What it does |
|----------|---------|-------------|
| **CI** | Push/PR to `main` | Lint, build, test (Ubuntu + macOS), coverage gate, example smoke tests, eval gate, Docker build |
| **Security** | Push/PR to `main`, nightly schedule | `govulncheck`, gosec (SAST), CodeQL (Go), Trivy filesystem scan — SARIF uploaded to the Security tab |
| **Release** | Tag `v*.*.*` | Test gate, build 6 platform binaries, source + image SBOM (SPDX), Trivy image scan, keyless cosign signing, GitHub Release with checksums, publish Go module, push signed image to GHCR |

Cut a release: `git tag v0.2.0 && git push origin v0.2.0`

---

## Contributing

1. Fork and create a feature branch from `main`
2. Follow Go conventions — `go vet`, `gofmt`, wrap errors with `%w`
3. No `init()` functions, no global state, `context.Context` first on I/O methods
4. Table-driven tests in `*_test.go` files

---

## License

[MIT](LICENSE)
