# Chronos Framework — Implementation Roadmap & Tracker

> This file tracks all implementation items by priority. Agents mark items `[x]` when complete.
> Last updated: 2026-03-23T12:00:00Z

## How to Use This File

- Items are organized **P0 → P1 → P2 → P3** (implement in order).
- Each item has: checkbox, ID, title, location, acceptance criteria.
- When an agent completes an item, it changes `[ ]` to `[x]` and appends `<!-- done: YYYY-MM-DD -->`.
- Run `grep -c "\[x\]" ROADMAP.md` to see completion count.
- Run `grep -c "\[ \]" ROADMAP.md` to see remaining count.

---

## Status Summary

| Priority | Total | Done | Remaining |
|----------|-------|------|-----------|
| P0       | 16    | 16   | 0         |
| P1       | 28    | 28   | 0         |
| P2       | 30    | 19   | 11        |
| P3       | 27    | 5    | 22        |
| **Total**| **101** | **68** | **33** |

---

## P0 — Critical Bugs & Wiring Fixes

> Must fix before any new feature work. These are broken or incomplete behaviors
> in existing code that undermine framework integrity.

### P0-A: Existing Code Bugs

- [x] **P0-001** — Fix Redis Storage list methods returning empty slices <!-- done: 2026-03-23 -->
  - **Location:** `storage/adapters/redis/redis.go` (~lines 128-180)
  - **Criteria:** `ListSessions`, `ListMemory`, `ListAuditLogs`, `ListTraces`, `ListEvents`, `ListCheckpoints` return real data from Redis (use SCAN/KEYS + HGETALL or sorted sets). Add unit test with miniredis.

- [x] **P0-002** — Fix RedisVector Search result parsing <!-- done: 2026-03-23 -->
  - **Location:** `storage/adapters/redisvector/redisvector.go` (~line 78-91)
  - **Criteria:** Parse the `FT.SEARCH` response into `[]storage.SearchResult` with score and metadata. Currently returns `[]` after issuing the command. Add unit test.

- [x] **P0-003** — Fix RetryHook to actually retry model calls <!-- done: 2026-03-23 -->
  - **Location:** `engine/hooks/retry.go`
  - **Criteria:** When a model call fails, the hook retries up to `MaxRetries` times with configurable backoff. Currently only sets metadata. The retry must wrap the actual model call, not just annotate. Add test.

- [x] **P0-004** — Wire NumHistoryRuns into Chat/ChatWithSession <!-- done: 2026-03-23 -->
  - **Location:** `sdk/agent/agent.go`
  - **Criteria:** When `NumHistoryRuns > 0`, load the last N runs from `Storage` (using `ListEvents` or session history) and inject them into the message context before calling the model. Currently the field exists but is never read during execution.

- [x] **P0-005** — Pass actual JSON Schema in OutputSchema to model API <!-- done: 2026-03-23 -->
  - **Location:** `sdk/agent/agent.go`
  - **Criteria:** When `OutputSchema` is set, pass the schema object to the model provider (OpenAI's `response_format.json_schema`, Anthropic's tool-use schema pattern). Currently only sets `response_format: "json_object"` without the schema. Validate response against schema before returning.

### P0-B: Critical Wiring Gaps

- [x] **P0-006** — Connect graph Runner to SSE Broker <!-- done: 2026-03-24 -->
  - **Location:** `engine/graph/runner.go`, `engine/stream/stream.go`, `os/server.go`
  - **Criteria:** When Runner executes nodes, it publishes events (node_start, node_end, tool_call, model_call, error) to `stream.Broker`. SSE endpoint subscribers receive these events in real-time. Add event types to `engine/stream/`.

- [x] **P0-007** — Wire trace.Collector into agent/graph execution <!-- done: 2026-03-24 -->
  - **Location:** `os/trace/trace.go`, `sdk/agent/agent.go`, `engine/graph/runner.go`
  - **Criteria:** `StartSpan`/`EndSpan` called during agent Chat, graph node execution, tool calls, and model calls. Traces stored via `storage.InsertTrace`. Trace parent-child hierarchy preserved.

- [x] **P0-008** — Fix CLI `sessions resume` (currently no-op) <!-- done: 2026-03-24 -->
  - **Location:** `cli/cmd/root.go`
  - **Criteria:** Load session by ID from storage, restore checkpoint, resume agent execution from last state. Print resumed output.

- [x] **P0-009** — Fix CLI `config set`/`config model` (currently prints guidance only) <!-- done: 2026-03-24 -->
  - **Location:** `cli/cmd/root.go`
  - **Criteria:** Persist config changes to `~/.chronos/config.yaml` or `$CHRONOS_CONFIG`. Reload on next CLI invocation.

### P0-C: Testing Foundation

- [x] **P0-010** — Add unit tests for `sdk/agent/` (Chat, Execute, Run) <!-- done: 2026-03-24 -->
  - **Location:** `sdk/agent/agent_test.go`
  - **Criteria:** Table-driven tests covering: basic chat, chat with tools, chat with guardrails, chat with memory, chat with knowledge, structured output. Use mock provider. Minimum 15 test cases.

- [x] **P0-011** — Add unit tests for `engine/graph/` (StateGraph, Runner) <!-- done: 2026-03-24 -->
  - **Location:** `engine/graph/graph_test.go`, `engine/graph/runner_test.go`
  - **Criteria:** Test graph compilation (valid/invalid), node execution, conditional edges, interrupt nodes, checkpointing, resume from checkpoint. Minimum 12 test cases.

- [x] **P0-012** — Add unit tests for `sdk/team/` (Team orchestration) <!-- done: 2026-03-24 -->
  - **Location:** `sdk/team/team_test.go`
  - **Criteria:** Test sequential, parallel, coordinator, and router strategies with mock agents. Minimum 8 test cases.

- [x] **P0-013** — Add unit tests for `engine/hooks/` (all hooks) <!-- done: 2026-03-24 -->
  - **Location:** `engine/hooks/hooks_test.go`
  - **Criteria:** Test each hook (retry, ratelimit, cache, cost, metrics) individually and as a chain. Minimum 10 test cases.

- [x] **P0-014** — Add unit tests for `engine/guardrails/` <!-- done: 2026-03-24 -->
  - **Location:** `engine/guardrails/guardrails_test.go`
  - **Criteria:** Test BlocklistGuardrail, MaxLengthGuardrail, Engine with input/output rules. Minimum 8 test cases.

- [x] **P0-015** — Add unit tests for `sdk/memory/` and `sdk/knowledge/` <!-- done: 2026-03-24 -->
  - **Location:** `sdk/memory/memory_test.go`, `sdk/knowledge/vectordb_test.go`
  - **Criteria:** Test memory CRUD, extraction, VectorKnowledge search with mock vector store and embeddings provider. Minimum 10 test cases.

- [x] **P0-016** — Add integration tests for `storage/adapters/sqlite/` <!-- done: 2026-03-24 -->
  - **Location:** `storage/adapters/sqlite/sqlite_test.go`
  - **Criteria:** Test all 18 Storage methods end-to-end with `:memory:` SQLite. Verify Migrate creates tables, CRUD works correctly, list operations filter properly. Expand existing test file.

---

## P1 — Core Feature Gaps (Competitive Parity)

> Features needed to match the capabilities of Agno, LangGraph, and DeepAgents.
> Implement after all P0 items are complete.

### P1-A: MCP (Model Context Protocol) Support

- [x] **P1-001** — MCP client implementation <!-- done: 2026-03-24 -->
  - **Location:** `engine/mcp/client.go` (new package)
  - **Criteria:** Connect to MCP server via stdio or HTTP SSE. List available tools and resources. Invoke tools with JSON arguments. Return results. Implement `initialize`, `tools/list`, `tools/call`, `resources/list`, `resources/read` methods per MCP spec.

- [x] **P1-002** — MCP tools integration with agent <!-- done: 2026-03-24 -->
  - **Location:** `sdk/agent/agent.go`, `engine/mcp/adapter.go`
  - **Criteria:** Agent can accept MCP server URLs. On initialization, fetch tools from MCP server and register them in the tool registry. Tool calls route through MCP client transparently.

### P1-B: Subgraphs & Graph Composition

- [x] **P1-003** — Subgraph support (graphs as nodes) <!-- done: 2026-03-24 -->
  - **Location:** `engine/graph/subgraph.go`
  - **Criteria:** `AddSubgraph(id string, sub *CompiledGraph)` registers a compiled graph as a node. Runner executes the subgraph when the node is reached, passing state in/out. Supports different state schemas between parent and child (via mapping function).

- [x] **P1-004** — Parallel fan-out / fan-in <!-- done: 2026-03-24 -->
  - **Location:** `engine/graph/parallel.go`
  - **Criteria:** `FanOut(branches []NodeFunc, merge MergeFunc)` fans out to multiple nodes concurrently. Execution waits for all to complete before merging state and continuing. State merge uses configurable `MergeFunc`.

- [x] **P1-005** — State reducers for graph state <!-- done: 2026-03-24 -->
  - **Location:** `engine/graph/reducer.go`
  - **Criteria:** Define `Reducer` interface with `Reduce(existing, update any) any`. Built-in reducers: `ReplaceReducer` (default), `AppendReducer` (for slices), `MergeMapReducer` (for maps). Graph state schema can associate reducers with keys.

### P1-C: Time Travel

- [x] **P1-006** — Replay from checkpoint <!-- done: 2026-03-24 -->
  - **Location:** `engine/graph/runner.go`
  - **Criteria:** `ReplayFrom(ctx, checkpointID)` loads checkpoint state, emits replay_start event, and re-executes from the checkpoint node forward. Results may differ due to non-deterministic LLM calls.

- [x] **P1-007** — Fork from checkpoint (branch with modified state) <!-- done: 2026-03-24 -->
  - **Location:** `engine/graph/runner.go`
  - **Criteria:** `ForkFrom(ctx, checkpointID, stateUpdate map[string]any)` creates a new session branch from the checkpoint with modified state. Original checkpoint history is preserved. New execution continues independently.

### P1-D: Advanced Streaming

- [x] **P1-008** — Streaming modes (values, updates, custom, debug) <!-- done: 2026-03-24 -->
  - **Location:** `engine/stream/modes.go`
  - **Criteria:** `StreamMode` enum with `Values`, `Updates`, `Custom`, `Messages`, `Debug`. `StreamConfig.ShouldInclude(eventType)` filters events per mode. 6 test cases.

- [x] **P1-009** — Custom event emission from tools/nodes <!-- done: 2026-03-24 -->
  - **Location:** `engine/stream/emit.go`
  - **Criteria:** `NodeFunc` and tool handlers can emit custom events via a channel or context helper: `stream.Emit(ctx, "my_event", data)`. Events are published to the Broker for SSE consumers.

### P1-E: Human-in-the-Loop (HITL) Enhancements

- [x] **P1-010** — Tool confirmation required <!-- done: 2026-03-24 -->
  - **Location:** `engine/tool/registry.go`
  - **Criteria:** `Definition.RequiresConfirmation bool`. When set, approval handler is called before execution. Denied confirmation returns error.

- [x] **P1-011** — User input required (agent pauses for external input) <!-- done: 2026-03-24 -->
  - **Location:** `engine/tool/registry.go`
  - **Criteria:** `Definition.RequiresUserInput bool`. Registry calls `UserInputFunc` handler and injects result as `__user_input__` in tool args.

- [x] **P1-012** — State inspection & modification mid-run <!-- done: 2026-03-24 -->
  - **Location:** `os/server.go`
  - **Criteria:** `GET /api/sessions/state` reads current state. `POST /api/sessions/state` modifies state via checkpoint. Runner resumes from modified state.

### P1-F: Context Management

- [x] **P1-013** — Auto-summarization when context exceeds threshold <!-- done: 2026-03-24 -->
  - **Location:** `engine/model/summarizer.go`
  - **Criteria:** When message history exceeds `ContextConfig.SummarizeThreshold` fraction of `MaxContextTokens`, invoke summarizer to compress older messages. Preserve recent messages and system prompt. Replace older history with summary message.

- [x] **P1-014** — Large tool result eviction <!-- done: 2026-03-24 -->
  - **Location:** `sdk/agent/context.go`
  - **Criteria:** `EvictLargeResult` stores oversized results in storage, returns truncated preview with storage key. `ReadStoredResult` retrieves full result. 3 test cases.

- [x] **P1-015** — Tool call compression <!-- done: 2026-03-24 -->
  - **Location:** `sdk/agent/context.go`
  - **Criteria:** `CompressToolCalls(messages, maxCalls)` removes oldest tool call/result pairs, preserves system/user messages. `ContextConfig.MaxToolCallsFromHistory` field added. 4 test cases.

### P1-G: Authentication & Security for API Server

- [x] **P1-016** — JWT authentication middleware <!-- done: 2026-03-24 -->
  - **Location:** `os/auth/jwt.go`
  - **Criteria:** HMAC-SHA256 JWT validation, UserClaims extraction, context injection, skip paths, expiry check. 7 test cases.

- [x] **P1-017** — API key authentication <!-- done: 2026-03-24 -->
  - **Location:** `os/auth/apikey.go`
  - **Criteria:** Constant-time key comparison, configurable header, scope-based keys, skip paths. 5 test cases.

- [x] **P1-018** — RBAC (role-based access control) <!-- done: 2026-03-24 -->
  - **Location:** `os/auth/auth.go`
  - **Criteria:** Role hierarchy (admin > user > viewer), `CheckPermission(claims, required)`, `RequireRole` middleware. 7 test cases.

- [x] **P1-019** — CORS configuration <!-- done: 2026-03-24 -->
  - **Location:** `os/middleware/cors.go`
  - **Criteria:** Configurable CORS middleware with origin allow-list, credentials, max-age, preflight handling. 4 test cases.

- [x] **P1-020** — Rate limiting middleware <!-- done: 2026-03-24 -->
  - **Location:** `os/middleware/ratelimit.go`
  - **Criteria:** Fixed-window rate limiter per key (IP or custom). X-RateLimit-* headers, 429 when exceeded. 5 test cases.

### P1-H: Evaluation Framework

- [x] **P1-021** — Eval runner infrastructure <!-- done: 2026-03-24 -->
  - **Location:** `evals/eval.go`
  - **Criteria:** `Eval` interface, `EvalResult`, `Suite` with aggregation, `ExactMatchEval`, `ContainsEval`. 7 test cases.

- [x] **P1-022** — Accuracy eval (LLM-as-judge) <!-- done: 2026-03-24 -->
  - **Location:** `evals/accuracy.go`
  - **Criteria:** LLM judge scoring with fallback to word-overlap. Configurable rubric. 5 test cases.

- [x] **P1-023** — Reliability eval (tool call correctness) <!-- done: 2026-03-24 -->
  - **Location:** `evals/reliability.go`
  - **Criteria:** Ordered tool call matching with argument comparison. Score = matched/expected. 5 test cases.

- [x] **P1-024** — Performance eval (latency & token usage) <!-- done: 2026-03-24 -->
  - **Location:** `evals/performance.go`
  - **Criteria:** Latency + token measurement with optional baselines. Regression detection. 6 test cases.

- [x] **P1-025** — Eval CLI command <!-- done: 2026-03-24 -->
  - **Location:** `cli/cmd/root.go`
  - **Criteria:** `chronos eval list` discovers suites, `chronos eval run <suite.yaml>` loads and runs evals.

### P1-I: In-Memory Storage Adapter

- [x] **P1-026** — In-memory Storage adapter <!-- done: 2026-03-24 -->
  - **Location:** `storage/adapters/memory/memory.go`
  - **Criteria:** Implements full `storage.Storage` interface using Go maps with sync.RWMutex. No external dependencies. Suitable for testing and development. `Migrate` is a no-op. Include `_test.go`.

### P1-J: Health & Lifecycle

- [x] **P1-027** — Health check endpoints <!-- done: 2026-03-24 -->
  - **Location:** `os/server.go`
  - **Criteria:** `GET /health` returns `{"status":"ok"}` with 200. `GET /health/live` (liveness: process is running). `GET /health/ready` (readiness: storage is connected). Returns 503 when not ready.

- [x] **P1-028** — Graceful shutdown <!-- done: 2026-03-24 -->
  - **Location:** `os/server.go`
  - **Criteria:** On SIGTERM/SIGINT: stop accepting new requests, drain in-flight requests (configurable timeout), close storage connections, exit cleanly.

---

## P2 — Ecosystem & Developer Experience

> Features that make the framework practical and pleasant to use.
> Implement after P0 and P1 are substantially complete.

### P2-A: Built-in Toolkits

- [x] **P2-001** — Calculator tool <!-- done: 2026-03-24 -->
  - **Location:** `engine/tool/builtins/calculator.go` (new)
  - **Criteria:** Evaluate mathematical expressions. Supports +, -, *, /, ^, (), sqrt, sin, cos, log. Returns numeric result. JSON schema describes input as expression string.

- [x] **P2-002** — Shell tool <!-- done: 2026-03-24 -->
  - **Location:** `engine/tool/builtins/shell.go` (new)
  - **Criteria:** Execute shell commands with configurable timeout, working directory, and allowed commands list. Returns stdout, stderr, exit code. Sandbox integration for safety. Permission: `dangerous`.

- [x] **P2-003** — File tools (read, write, list, glob, grep) <!-- done: 2026-03-24 -->
  - **Location:** `engine/tool/builtins/file.go` (new)
  - **Criteria:** `read_file(path)`, `write_file(path, content)`, `list_dir(path)`, `glob(pattern)`, `grep(pattern, path)`. Configurable root directory and path restrictions. Permission: `filesystem`.

- [x] **P2-004** — Web search tool (DuckDuckGo) <!-- done: 2026-03-24 -->
  - **Location:** `engine/tool/builtins/websearch.go` (new)
  - **Criteria:** Search DuckDuckGo API, return top N results with title, URL, snippet. No API key required. Configurable result count.

- [x] **P2-005** — SQL tool (query execution) <!-- done: 2026-03-24 -->
  - **Location:** `engine/tool/builtins/sql.go` (new)
  - **Criteria:** Execute SQL queries against a configured database. Returns results as JSON array. Read-only by default, write requires explicit permission. Configurable connection string.

- [x] **P2-006** — HTTP request tool <!-- done: 2026-03-24 -->
  - **Location:** `engine/tool/builtins/http.go` (new)
  - **Criteria:** Make HTTP requests (GET, POST, PUT, DELETE) to external APIs. Configurable headers, body, timeout. Returns status code, headers, body. Allowlist for domains.

- [x] **P2-007** — Sleep / wait tool <!-- done: 2026-03-24 -->
  - **Location:** `engine/tool/builtins/sleep.go` (new)
  - **Criteria:** Pause execution for a specified duration. Useful for rate limiting and polling patterns. Max duration configurable.

### P2-B: Document Loaders for Knowledge Base

- [x] **P2-008** — Plain text loader <!-- done: 2026-03-24 -->
  - **Location:** `sdk/knowledge/loaders/text.go` (new package)
  - **Criteria:** Load `.txt` and `.md` files. Split into chunks by configurable size (default 1000 tokens) with overlap (default 200 tokens). Return `[]Document` with content and metadata (source, chunk_index).

- [x] **P2-009** — PDF loader <!-- done: 2026-03-24 -->
  - **Location:** `sdk/knowledge/loaders/pdf.go`
  - **Criteria:** Extract text from PDF files using a Go PDF library (e.g., `pdfcpu` or `unipdf`). Split into chunks. Return `[]Document`. Handle multi-page documents.

- [x] **P2-010** — CSV/JSON loader <!-- done: 2026-03-24 -->
  - **Location:** `sdk/knowledge/loaders/structured.go`
  - **Criteria:** Load CSV and JSON files. Each row/object becomes a document. Configurable content field selection. Metadata from other fields.

- [x] **P2-011** — Web page loader (URL scraper) <!-- done: 2026-03-24 -->
  - **Location:** `sdk/knowledge/loaders/web.go`
  - **Criteria:** Fetch URL, extract main content (strip HTML boilerplate), chunk text. Support for JavaScript-rendered pages is optional. Return `[]Document` with URL as source.

- [x] **P2-012** — Chunking strategies <!-- done: 2026-03-24 -->
  - **Location:** `sdk/knowledge/chunker.go` (new file)
  - **Criteria:** `Chunker` interface with `Chunk(text string) []Chunk`. Built-in: `FixedSizeChunker` (by token/char count with overlap), `RecursiveSplitChunker` (by paragraph → sentence → word), `SemanticChunker` (by embedding similarity). Configurable chunk size and overlap.

### P2-C: Multimodal Message Support

- [x] **P2-013** — Image input support in Message type <!-- done: 2026-03-24 -->
  - **Location:** `engine/model/provider.go`
  - **Criteria:** Extend `Message` with `Images []ImageContent` where `ImageContent` has `URL string` or `Base64 string` + `MimeType`. OpenAI and Anthropic providers handle image content in requests.

- [ ] **P2-014** — Audio input/output support
  - **Location:** `engine/model/provider.go`
  - **Criteria:** Extend `Message` with `Audio []AudioContent`. Support for Whisper-style transcription input and TTS output. Provider implementations for OpenAI audio models.

- [x] **P2-015** — File attachment support <!-- done: 2026-03-24 -->
  - **Location:** `engine/model/provider.go`
  - **Criteria:** Extend `Message` with `Files []FileContent` for document/file uploads to models that support them (Gemini, Claude). Provider implementations handle file encoding.

### P2-D: Functional API (Go-idiomatic alternative to Graph API)

- [x] **P2-016** — Entrypoint registration (equivalent to @entrypoint) <!-- done: 2026-03-24 -->
  - **Location:** `engine/graph/functional.go` (new file)
  - **Criteria:** `RegisterEntrypoint(name string, fn func(ctx context.Context, input any) (any, error))` wraps a Go function as a graph entrypoint. Integrates with checkpointing and durable execution. Returns a `CompiledGraph` that can be used anywhere a graph is expected.

- [x] **P2-017** — Task registration (equivalent to @task) <!-- done: 2026-03-24 -->
  - **Location:** `engine/graph/functional.go`
  - **Criteria:** `RegisterTask(name string, fn func(ctx context.Context, input any) (any, error))` marks a function as a checkpoint-able task. Results are saved automatically. If a task was already completed in a previous run (via checkpoint), its cached result is returned.

### P2-E: Graph Visualization

- [x] **P2-018** — Mermaid diagram export <!-- done: 2026-03-24 -->
  - **Location:** `engine/graph/visualize.go` (new file)
  - **Criteria:** `CompiledGraph.ToMermaid() string` generates a Mermaid flowchart definition. Nodes show IDs, interrupt nodes are highlighted, conditional edges show labels. Output is copy-pasteable into Mermaid renderers.

- [x] **P2-019** — DOT (Graphviz) export <!-- done: 2026-03-24 -->
  - **Location:** `engine/graph/visualize.go`
  - **Criteria:** `CompiledGraph.ToDOT() string` generates DOT format. Nodes colored by type (start/end/normal/interrupt). Edges labeled for conditionals.

### P2-F: Observability

- [x] **P2-020** — OpenTelemetry integration <!-- done: 2026-03-24 -->
  - **Location:** `os/trace/otel.go` (new file)
  - **Criteria:** `OTelCollector` implements trace collection using OpenTelemetry SDK. Exports spans to configured OTLP endpoint. Agent/graph/tool operations create OTel spans with proper parent-child relationships and attributes.

- [x] **P2-021** — Debug mode for agents <!-- done: 2026-03-24 -->
  - **Location:** `sdk/agent/agent.go`
  - **Criteria:** `Agent.Debug bool` flag. When set, logs detailed execution: every model call (prompt + response), tool calls (args + result), guardrail checks, memory operations, knowledge searches. Uses structured logger.

- [x] **P2-022** — Metrics export (Prometheus format) <!-- done: 2026-03-24 -->
  - **Location:** `os/metrics/prometheus.go` (new file), `os/server.go`
  - **Criteria:** `GET /metrics` endpoint serving Prometheus-format metrics: `chronos_agent_runs_total`, `chronos_model_latency_seconds`, `chronos_tool_calls_total`, `chronos_tokens_used_total`, `chronos_active_sessions`. Hook-based collection.

### P2-G: Scheduler

- [x] **P2-023** — Cron job scheduler for agents <!-- done: 2026-03-24 -->
  - **Location:** `os/scheduler/scheduler.go` (new package)
  - **Criteria:** `Scheduler` manages cron-scheduled agent runs. Supports standard cron expressions (5-field). Each schedule specifies: agent ID, input message, session handling (new session per run or reuse). Schedule CRUD via API.

- [x] **P2-024** — Scheduler API endpoints <!-- done: 2026-03-24 -->
  - **Location:** `os/server.go`, `os/scheduler/`
  - **Criteria:** `POST /api/schedules`, `GET /api/schedules`, `DELETE /api/schedules/{id}`, `GET /api/schedules/{id}/history`. Schedules persist in storage.

### P2-H: Additional Guardrails

- [x] **P2-025** — PII detection guardrail <!-- done: 2026-03-24 -->
  - **Location:** `engine/guardrails/pii.go` (new file)
  - **Criteria:** Detect common PII patterns: email, phone, SSN, credit card, IP address. Configurable: block or redact. Regex-based for zero external dependencies.

- [x] **P2-026** — Prompt injection detection guardrail <!-- done: 2026-03-24 -->
  - **Location:** `engine/guardrails/injection.go` (new file)
  - **Criteria:** Detect common prompt injection patterns: "ignore previous instructions", role hijacking, delimiter injection. Pattern-matching based. Configurable sensitivity level.

### P2-I: Agent Features

- [x] **P2-027** — Dynamic instructions via function <!-- done: 2026-03-24 -->
  - **Location:** `sdk/agent/agent.go`
  - **Criteria:** `Agent.InstructionsFn func(ctx context.Context, state map[string]any) []string` allows generating instructions dynamically based on runtime state. Called before each model invocation. Static `Instructions` used as fallback.

- [x] **P2-028** — Few-shot learning support <!-- done: 2026-03-24 -->
  - **Location:** `sdk/agent/agent.go`
  - **Criteria:** `Agent.Examples []Example` where `Example` has `Input string` and `Output string`. Examples injected into context as user/assistant message pairs before the actual conversation. Configurable max examples count.

- [x] **P2-029** — Max iterations / recursion limit <!-- done: 2026-03-24 -->
  - **Location:** `sdk/agent/agent.go`
  - **Criteria:** `Agent.MaxIterations int` limits the tool-calling loop. When the agent exceeds this limit, it returns the last model response with a warning. Prevents infinite loops. Default: 25.

- [x] **P2-030** — Toolkit abstraction (grouped tools) <!-- done: 2026-03-24 -->
  - **Location:** `engine/tool/toolkit.go` (new file)
  - **Criteria:** `Toolkit` struct groups related `ToolDefinition`s with a shared name, description, and permission level. `agent.New().AddToolkit(tk)` registers all tools in the group. Toolkits can be enabled/disabled at runtime.

---

## P3 — Ecosystem Expansion

> Nice-to-have features that round out the framework and expand integrations.
> Implement when P0-P2 are stable.

### P3-A: Additional Model Providers

- [ ] **P3-001** — AWS Bedrock provider
  - **Location:** `engine/model/bedrock.go` (new file)
  - **Criteria:** Implement `Provider` using AWS Bedrock InvokeModel API. Support Claude, Titan, Llama models via Bedrock. Constructor takes AWS region + credentials.

- [ ] **P3-002** — Groq provider
  - **Location:** `engine/model/groq.go` (new file)
  - **Criteria:** Implement `Provider` using Groq API (OpenAI-compatible). Constructor takes API key. Support Llama, Mixtral models.

- [ ] **P3-003** — Together AI provider
  - **Location:** `engine/model/together.go` (new file)
  - **Criteria:** Implement `Provider` using Together API (OpenAI-compatible). Constructor takes API key.

- [ ] **P3-004** — Cohere provider
  - **Location:** `engine/model/cohere.go` (new file)
  - **Criteria:** Implement `Provider` for Cohere Chat API. Support Command models. Implement `EmbeddingsProvider` for Cohere embeddings.

- [ ] **P3-005** — DeepSeek provider
  - **Location:** `engine/model/deepseek.go` (new file)
  - **Criteria:** Implement `Provider` using DeepSeek API (OpenAI-compatible). Constructor takes API key. Support DeepSeek-V3 and reasoning models.

- [x] **P3-006** — Model-as-string syntax ("provider:model_id") <!-- done: 2026-03-24 -->
  - **Location:** `engine/model/resolve.go` (new file)
  - **Criteria:** `model.Resolve("openai:gpt-4o")` returns a configured `Provider` instance. Parse provider name, look up constructor, pass API key from environment. Supports all registered providers.

### P3-B: Additional Vector Stores

- [ ] **P3-007** — ChromaDB vector store
  - **Location:** `storage/adapters/chroma/chroma.go` (new)
  - **Criteria:** Implement `VectorStore` using ChromaDB REST API. Support Upsert, Search, Delete, CreateCollection. Include test.

- [ ] **P3-008** — PgVector vector store
  - **Location:** `storage/adapters/pgvector/pgvector.go` (new)
  - **Criteria:** Implement `VectorStore` using PostgreSQL with pgvector extension. Use `database/sql` with pgx driver. Support cosine similarity search. Include test.

- [ ] **P3-009** — LanceDB vector store
  - **Location:** `storage/adapters/lancedb/lancedb.go` (new)
  - **Criteria:** Implement `VectorStore` using LanceDB Go client (or REST API). Embedded/serverless vector DB. Include test.

### P3-C: Additional Embeddings Providers

- [ ] **P3-010** — Cohere embeddings provider
  - **Location:** `engine/model/cohere_embeddings.go` (new file)
  - **Criteria:** Implement `EmbeddingsProvider` using Cohere Embed API. Constructor takes API key and model name.

- [ ] **P3-011** — Azure OpenAI embeddings provider
  - **Location:** `engine/model/azure_embeddings.go` (new file)
  - **Criteria:** Implement `EmbeddingsProvider` using Azure OpenAI Embeddings API. Constructor takes endpoint, API key, deployment name.

- [ ] **P3-012** — Google embeddings provider
  - **Location:** `engine/model/google_embeddings.go` (new file)
  - **Criteria:** Implement `EmbeddingsProvider` using Google textembedding-gecko model. Constructor takes API key or service account.

### P3-D: Interface Integrations

- [ ] **P3-013** — Slack bot interface
  - **Location:** `os/interfaces/slack/slack.go` (new package)
  - **Criteria:** Receive messages from Slack (via Events API or Socket Mode), route to configured agent, post response back to channel. Support threads, mentions, and DMs. Configurable bot token.

- [ ] **P3-014** — Discord bot interface
  - **Location:** `os/interfaces/discord/discord.go` (new package)
  - **Criteria:** Discord bot that listens for messages, routes to agent, responds. Support slash commands and message replies. Configurable bot token.

- [ ] **P3-015** — Telegram bot interface
  - **Location:** `os/interfaces/telegram/telegram.go` (new package)
  - **Criteria:** Telegram bot using long polling or webhooks. Route messages to agent, send responses. Support inline keyboards for HITL confirmations.

- [x] **P3-016** — Webhook interface (generic) <!-- done: 2026-03-24 -->
  - **Location:** `os/interfaces/webhook/webhook.go` (new package)
  - **Criteria:** Generic webhook endpoint that accepts POST with message payload, routes to agent, returns response. Configurable authentication (HMAC signature verification).

### P3-E: Advanced Multi-Agent Patterns

- [ ] **P3-017** — Swarm pattern (peer-to-peer handoff)
  - **Location:** `sdk/team/swarm.go` (new file)
  - **Criteria:** Agents can hand off directly to other agents without a central coordinator. `Handoff(targetAgent, taskDescription)` tool. Any agent can interact with the user. The active agent changes on handoff.

- [ ] **P3-018** — Hierarchical multi-level supervisors
  - **Location:** `sdk/team/hierarchy.go` (new file)
  - **Criteria:** A supervisor team can contain other supervisor teams as members, creating a tree structure. Top-level supervisor delegates to mid-level supervisors, which delegate to worker agents.

- [ ] **P3-019** — A2A protocol (agent-to-agent interop)
  - **Location:** `sdk/protocol/a2a/` (new package)
  - **Criteria:** Implement the A2A protocol for cross-framework agent communication. `A2AServer` exposes an agent as an A2A endpoint. `A2AClient` connects to external A2A agents. Support task creation, status polling, and streaming.

- [x] **P3-020** — Custom handoff tools with task instructions <!-- done: 2026-03-24 -->
  - **Location:** `sdk/team/handoff.go` (new file)
  - **Criteria:** When an agent hands off to another, it can provide structured task instructions (objective, context, constraints). The receiving agent sees these as its initial prompt. Reduces failed handoffs.

### P3-F: Reasoning & Advanced AI

- [x] **P3-021** — Reasoning tools (chain-of-thought) <!-- done: 2026-03-24 -->
  - **Location:** `engine/tool/builtins/reasoning.go` (new file)
  - **Criteria:** `think(thought string)` tool that allows the model to perform explicit reasoning steps. The thought is recorded in context but not shown to the user. Useful for complex multi-step analysis.

- [ ] **P3-022** — Separate reasoning model (two-model architecture)
  - **Location:** `sdk/agent/agent.go`
  - **Criteria:** `Agent.ReasoningModel Provider` field. When set, reasoning steps use a more capable (but slower) model, while final responses use the primary model. Configurable which steps use which model.

### P3-G: Sandbox Enhancements

- [ ] **P3-023** — Container pooling (pre-warmed containers)
  - **Location:** `sandbox/pool.go` (new file)
  - **Criteria:** `ContainerPool` maintains N pre-warmed containers. `Acquire()` returns a ready container instantly. `Release()` returns it to the pool. Configurable pool size, max idle time. Reduces cold-start latency.

- [ ] **P3-024** — Pluggable sandbox backends
  - **Location:** `sandbox/sandbox.go`
  - **Criteria:** `Sandbox` interface implemented by: `ProcessSandbox` (existing), `ContainerSandbox` (existing), `WASMSandbox` (new, using Wazero), `K8sJobSandbox` (new, using Kubernetes Jobs). Factory function selects backend by config string.

### P3-H: CLI Enhancements

- [x] **P3-025** — Non-interactive mode (pipe tasks) <!-- done: 2026-03-24 -->
  - **Location:** `cli/cmd/root.go`
  - **Criteria:** `chronos run -n "task description"` runs the agent non-interactively. Reads from stdin if piped. Outputs to stdout. Exit code 0 on success, 1 on failure. Suitable for scripting.

- [ ] **P3-026** — CLI monitor TUI
  - **Location:** `cli/cmd/monitor.go` (new file)
  - **Criteria:** Live terminal UI showing: active sessions (count + list), recent tool calls, token usage, model latency, error rate. Refreshes periodically. Uses a Go TUI library (e.g., `bubbletea`).

### P3-I: Production Hardening

- [ ] **P3-027** — Database migration framework
  - **Location:** `storage/migrate/migrate.go` (new package)
  - **Criteria:** Versioned migrations for SQL backends (SQLite, Postgres). Migration files in `storage/migrate/migrations/`. `Migrate(ctx, db)` applies pending migrations. `Status(ctx, db)` shows current version. `Rollback(ctx, db)` reverts last migration. Track applied migrations in a `_migrations` table.

---

## Dependency Graph

```
P0 (bugs + tests) ─────────────────────────────────────────────┐
  │                                                              │
  ├── P0-001..005 (fix existing bugs)                           │
  ├── P0-006..009 (wiring fixes)                                │
  └── P0-010..016 (testing foundation)                          │
                                                                 │
P1 (core features) ◄────────────────────────────────────────────┘
  │
  ├── P1-001..002 (MCP) ── depends on: P0 tool registry tests
  ├── P1-003..005 (subgraphs) ── depends on: P0 graph tests
  ├── P1-006..007 (time travel) ── depends on: P0 graph tests
  ├── P1-008..009 (streaming) ── depends on: P0-006 (Runner→Broker)
  ├── P1-010..012 (HITL) ── depends on: P0-006, P0-007
  ├── P1-013..015 (context mgmt) ── depends on: P0-004, P0-005
  ├── P1-016..020 (auth/security) ── depends on: P0 server tests
  ├── P1-021..025 (evals) ── depends on: P0-010 (agent tests)
  ├── P1-026 (in-memory storage) ── no deps
  └── P1-027..028 (health/lifecycle) ── no deps
                                                                 
P2 (ecosystem) ◄─────── depends on: P1 substantially complete
  │
  ├── P2-001..007 (toolkits) ── depends on: P1 tool confirmation
  ├── P2-008..012 (doc loaders) ── depends on: P0 knowledge tests
  ├── P2-013..015 (multimodal) ── depends on: P0 provider tests
  ├── P2-016..017 (functional API) ── depends on: P1 subgraphs
  ├── P2-018..019 (visualization) ── depends on: P1 subgraphs
  ├── P2-020..022 (observability) ── depends on: P0-007 (tracing)
  ├── P2-023..024 (scheduler) ── depends on: P1 auth
  ├── P2-025..026 (guardrails) ── no deps
  ├── P2-027..030 (agent features) ── depends on: P0-010
  
P3 (expansion) ◄─────── depends on: P2 substantially complete
  │
  ├── P3-001..006 (model providers) ── depends on: P0 provider tests
  ├── P3-007..009 (vector stores) ── depends on: P0-002 fix pattern
  ├── P3-010..012 (embeddings) ── no deps
  ├── P3-013..016 (interfaces) ── depends on: P1 auth + P2 scheduler
  ├── P3-017..020 (multi-agent) ── depends on: P0 team tests
  ├── P3-021..022 (reasoning) ── depends on: P0-010
  ├── P3-023..024 (sandbox) ── no deps
  ├── P3-025..026 (CLI) ── depends on: P0-008, P0-009
  └── P3-027 (migrations) ── depends on: P0-016
```

---

## Completion Log

> Agents append entries here when completing items.

| Date | Item ID | Agent | Notes |
|------|---------|-------|-------|
| 2026-03-23 | P0-001 | cursor-agent | Redis list methods now use sorted set indexes (ZADD/ZREVRANGE/ZRANGE) for all list operations. 6 functions fixed. Unit tests added. |
| 2026-03-23 | P0-002 | cursor-agent | RedisVector Search now parses FT.SEARCH RESP response into SearchResult with score, content, and metadata. Parser handles multi-result responses correctly. Unit tests added. |
| 2026-03-23 | P0-003 | cursor-agent | RetryHook now performs actual retries by re-invoking the model provider. Supports SleepFn injection for testing. Falls back to metadata-only signaling for backward compatibility when provider/request not in metadata. 12 test cases added. |
| 2026-03-23 | P0-004 | cursor-agent | NumHistoryRuns now loads past sessions from storage and injects user/assistant messages into context. Filters out system messages. Works gracefully when storage is nil. 5 test cases added. |
| 2026-03-23 | P0-005 | cursor-agent | OutputSchema now passes full JSON Schema via Metadata["json_schema"] with ResponseFormat "json_schema". Added validateAgainstSchema for required fields and type checking. Applied to both Chat and ChatWithSession. 13+ test cases added. |
