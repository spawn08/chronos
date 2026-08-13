---
title: "Configuration"
---


Chronos agents are configured via YAML. This page describes the config file layout, search order, and all supported options.

## Config file search order

The CLI and `agent.LoadFile("")` look for config in this order:

1. **`CHRONOS_CONFIG`** — If set, use this path. Overrides all other locations.
2. **`.chronos/agents.yaml`** — Project-level config (current directory).
3. **`agents.yaml`** — Config in the current directory.
4. **`~/.chronos/agents.yaml`** — User-level global config.

Both `.yaml` and `.yml` extensions are supported. Configuration parsing is strict: unknown fields, duplicate agent IDs, invalid permission modes, and invalid reasoning settings return a path-aware error instead of being ignored. Validate without running an agent:

```bash
chronos -c ./agents.yaml config validate
# Configuration is valid: 2 agent(s), 1 team(s).
```

To force a specific file:

```bash
export CHRONOS_CONFIG=/path/to/agents.yaml
chronos repl
```

## Full YAML structure

```yaml
defaults:
  model:
    provider: openai
    model: gpt-5.5
    api_key: ${OPENAI_API_KEY}
    base_url: ""
    org_id: ""
    timeout_sec: 60
    # Azure-specific:
    endpoint: ""
    deployment: ""
    api_version: "2024-06-01"
  storage:
    backend: sqlite
    dsn: chronos.db
  system_prompt: ""
  num_history_runs: 0
  stream: true
  debug: false
  tracing: false
  permission_mode: prompt          # prompt, auto_approve, deny
  reasoning:
    strategy: none                 # none, cot, reflection
    native: false
    effort: ""                     # low, medium, high when native: true
    budget_tokens: 0
    summary: false
  context:
    max_tokens: 0
    summarize_threshold: 0.8
    preserve_recent_turns: 5

agents:
  - id: my-agent
    name: My Agent
    description: Optional description
    model:
      provider: openai
      model: gpt-5.5
      api_key: ${OPENAI_API_KEY}
      base_url: ""
      org_id: ""
      timeout_sec: 60
      endpoint: ""
      deployment: ""
      api_version: ""
    storage:
      backend: sqlite
      dsn: chronos.db
    system_prompt: |
      Your system prompt here.
    instructions:
      - Additional instruction 1
      - Additional instruction 2
    tools:
      - name: file_read
        permission: allow          # allow, require_approval, deny
      - name: file_write
        permission: require_approval
        requires_confirmation: false
    capabilities: []
    mcp_servers: []                # Model Context Protocol servers (see guide)
    sub_agents: []
    output_schema: {}
    num_history_runs: 0
    stream: true                   # CLI/REPL default; flags can override
    debug: false
    tracing: false
    permission_mode: prompt        # prompt, auto_approve, deny
    reasoning:
      strategy: none
      native: false
      effort: ""
      budget_tokens: 0
      summary: false
    context:
      max_tokens: 0
      summarize_threshold: 0.8
      preserve_recent_turns: 5

teams:
  - id: my-team
    name: My Team
    strategy: sequential           # sequential, parallel, router, coordinator
    agents:                        # agent IDs (order matters for sequential)
      - agent-1
      - agent-2
    coordinator: ""                # agent ID (coordinator strategy only)
    max_concurrency: 0             # parallel strategy; 0 = unbounded
    max_iterations: 1              # coordinator strategy; planning loops
    error_strategy: ""             # fail_fast, collect, best_effort
```

## ModelConfig

| Field | Description |
|-------|-------------|
| `provider` | One of: `openai`, `anthropic`, `gemini`, `mistral`, `ollama`, `azure`, `groq`, `together`, `deepseek`, `openrouter`, `fireworks`, `perplexity`, `anyscale`, `compatible` |
| `model` | Model ID (e.g., `gpt-5.5`, `claude-opus-4-8`, `llama3.3`) |
| `api_key` | API key; supports `${VAR}` expansion |
| `base_url` | Custom base URL for compatible providers |
| `org_id` | OpenAI organization ID |
| `timeout_sec` | Request timeout in seconds |
| `endpoint` | Azure resource endpoint |
| `deployment` | Azure deployment name |
| `api_version` | Azure API version (e.g., `2024-06-01`) |

## Agent runtime fields

| Field | Description |
|-------|-------------|
| `stream` | Preferred CLI response mode. `true` streams tokens; `false` waits for the complete response. `--stream` / `--no-stream` override it. |
| `debug` | Emit detailed agent execution logs to stderr. |
| `tracing` | Persist model, tool, and graph spans through the agent's configured storage. |
| `permission_mode` | `prompt`, `auto_approve`, or `deny` for approval-gated tools. Explicit tool `deny` is never bypassed. |
| `reasoning.strategy` | Prompt strategy: `none`, `cot`, or `reflection`. |
| `reasoning.native` | Enable provider-native reasoning/thinking where supported. |
| `reasoning.effort` | Native reasoning effort (`low`, `medium`, `high`) for providers that expose it. |
| `reasoning.budget_tokens` | Thinking-token budget for Anthropic/Gemini-style APIs. |
| `reasoning.summary` | Make provider-approved reasoning output available separately from answer content and display streaming reasoning on CLI stderr. |

## Tool configuration

Each `tools` entry supports `name`, `description`, `parameters`, and these policy fields:

| Field | Description |
|-------|-------------|
| `permission` | `allow`, `require_approval`, or `deny`. When omitted, the built-in tool's safe default is preserved. |
| `requires_confirmation` | Explicitly require or remove a confirmation gate. |
| `requires_user_input` | Ask the registered user-input handler before execution. |

See [Tools & Function Calling](/guides/tools) and [CLI Runtime Controls](/guides/cli-runtime-controls).

## StorageConfig

| Field | Description |
|-------|-------------|
| `backend` | `sqlite` or `postgres` |
| `dsn` | Connection string or file path (e.g., `chronos.db` for SQLite) |

## Server storage environment variables

`chronos serve` (ChronosOS) selects its storage backend from environment
variables. The server uses **exactly one backend at a time**.

| Variable | Values / example | Purpose |
|----------|------------------|---------|
| `CHRONOS_STORAGE_BACKEND` | `sqlite` (default), `postgres`, `redis` | Which storage backend the server uses |
| `CHRONOS_DB_PATH` | `chronos.db` | SQLite file path (when backend = `sqlite`) |
| `CHRONOS_STORAGE_DSN` | `postgres://user:pass@host:5432/chronos?sslmode=disable` | Postgres connection string (when backend = `postgres`) |
| `CHRONOS_REDIS_URL` | `redis://host:6379/0` | Redis connection URL (when backend = `redis`) |

:::note Redis is a storage backend only
When `CHRONOS_STORAGE_BACKEND=redis`, Redis is used **only** as a durable-storage
backend. It is **not** used for scheduling or rate limiting.
:::

### Shared state (scheduler + rate limiter)

`CHRONOS_SHARED_STATE` controls whether the server uses store-backed shared
coordination: a scheduler where each cron job fires **exactly once across all
replicas**, and a **cluster-wide** SQL rate limiter.

| Backend | Default | Behaviour |
|---------|---------|-----------|
| `postgres` | on | Store-backed exactly-once scheduler + shared SQL rate limiter, automatically. Set `CHRONOS_SHARED_STATE=false` to opt out. |
| `sqlite` | off | Single-node. Set `CHRONOS_SHARED_STATE=true` to enable it. |
| `redis` | n/a | Never gets the shared scheduler or rate limiter, regardless of this value. |

For a ready-to-run local Postgres stack, see [Local Development](/getting-started/local-development/).

## MCP servers (`mcp_servers`)

Each entry connects the agent to a [Model Context Protocol](/guides/mcp) server whose tools are imported into the registry when `ConnectMCP` is called.

| Field | Description |
|-------|-------------|
| `name` | Logical server name (used in error messages) |
| `transport` | `stdio` (default) or `sse` (planned) |
| `command` | Executable to launch for stdio (e.g., `npx`, `uvx`); supports `${VAR}` |
| `args` | Arguments passed to the command; each supports `${VAR}` |
| `url` | Endpoint for SSE transport; supports `${VAR}` |

```yaml
    mcp_servers:
      - name: filesystem
        transport: stdio
        command: npx
        args: ["-y", "@modelcontextprotocol/server-filesystem", "."]
```

See the [MCP guide](/guides/mcp) for the full workflow.

## TeamConfig

| Field | Description |
|-------|-------------|
| `id` | Unique team identifier (used in `team run`) |
| `name` | Display name |
| `strategy` | `sequential`, `parallel`, `router`, or `coordinator` |
| `agents` | List of agent IDs (order matters for sequential) |
| `coordinator` | Agent ID for the coordinator strategy |
| `max_concurrency` | Max parallel goroutines (parallel strategy); `0` = unbounded |
| `max_iterations` | Max coordinator planning loops; default `1` |
| `error_strategy` | `fail_fast`, `collect`, or `best_effort` (parallel strategy) |

## Context management

Control context window behavior and summarization:

| Field | Description | Default |
|-------|-------------|---------|
| `context.max_tokens` | Override model default; `0` = use model default | 0 |
| `context.summarize_threshold` | Fraction of context window that triggers summarization | 0.8 |
| `context.preserve_recent_turns` | Number of recent user/assistant pairs to keep | 5 |

## Environment variable expansion

All string values support `${VAR}` syntax. Unset variables expand to empty strings.

```yaml
agents:
  - id: dev
    model:
      api_key: ${OPENAI_API_KEY}
    storage:
      dsn: ${CHRONOS_DB_PATH}
```

## Defaults inheritance

Values in `defaults` cascade to every agent. Agents override only the fields they specify.

```yaml
defaults:
  model:
    provider: openai
    api_key: ${OPENAI_API_KEY}
  storage:
    backend: sqlite
    dsn: chronos.db
  system_prompt: You are a helpful assistant.
  context:
    summarize_threshold: 0.8
    preserve_recent_turns: 5

agents:
  - id: dev
    name: Dev Agent
    model:
      model: gpt-5.5
    system_prompt: You are a senior engineer.

  - id: researcher
    name: Research Agent
    model:
      provider: anthropic
      model: claude-opus-4-8
      api_key: ${ANTHROPIC_API_KEY}
```

- `dev` inherits provider, api_key, storage, and context; overrides model and system_prompt.
- `researcher` overrides provider, model, and api_key; inherits storage and context.

## Supported providers

| Provider | Description |
|----------|-------------|
| `openai` | OpenAI GPT-5.5, GPT-5, GPT-4o, o-series (o3, o4-mini) |
| `anthropic` | Claude Opus 4.8, Sonnet 5, Haiku 4.5, Fable 5 |
| `gemini` | Google Gemini |
| `mistral` | Mistral AI |
| `ollama` | Local Ollama (no API key) |
| `azure` | Azure OpenAI |
| `groq` | Groq |
| `together` | Together AI |
| `deepseek` | DeepSeek |
| `openrouter` | OpenRouter |
| `fireworks` | Fireworks AI |
| `perplexity` | Perplexity |
| `anyscale` | Anyscale Endpoints |
| `compatible` | Any OpenAI-compatible endpoint (vLLM, TGI, LiteLLM, etc.) |

## Real-World Examples

For complete, runnable YAML configurations with step-by-step setup instructions, see the [YAML Agent Examples](/guides/yaml-examples/) guide:

- **Customer Support Router** — Three specialist agents with intelligent routing
- **Content Creation Pipeline** — Sequential research → write → edit workflow
- **Software Development Team** — Coordinator-driven task decomposition
- **Multi-Provider Setup** — Mix OpenAI, Anthropic, Gemini, and Ollama
- **Parallel Analysis Team** — Multiple perspectives on the same question
