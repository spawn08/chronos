---
title: "Configuration"
permalink: /getting-started/configuration/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

Chronos agents are configured via YAML. This page describes the config file layout, search order, and all supported options.

## Config file search order

The CLI and `agent.LoadFile("")` look for config in this order:

1. **`CHRONOS_CONFIG`** — If set, use this path. Overrides all other locations.
2. **`.chronos/agents.yaml`** — Project-level config (current directory).
3. **`agents.yaml`** — Config in the current directory.
4. **`~/.chronos/agents.yaml`** — User-level global config.

Both `.yaml` and `.yml` extensions are supported. To force a specific file:

```bash
export CHRONOS_CONFIG=/path/to/agents.yaml
go run ./cli/main.go repl
```

## Full YAML structure

```yaml
defaults:
  model:
    provider: openai
    model: gpt-4o
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
      model: gpt-4o
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
    tools: []
    capabilities: []
    sub_agents: []
    output_schema: {}
    num_history_runs: 0
    stream: false
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
| `model` | Model ID (e.g., `gpt-4o`, `claude-sonnet-4-6`, `llama3.3`) |
| `api_key` | API key; supports `${VAR}` expansion |
| `base_url` | Custom base URL for compatible providers |
| `org_id` | OpenAI organization ID |
| `timeout_sec` | Request timeout in seconds |
| `endpoint` | Azure resource endpoint |
| `deployment` | Azure deployment name |
| `api_version` | Azure API version (e.g., `2024-06-01`) |

## StorageConfig

| Field | Description |
|-------|-------------|
| `backend` | `sqlite` or `postgres` |
| `dsn` | Connection string or file path (e.g., `chronos.db` for SQLite) |

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
      model: gpt-4o
    system_prompt: You are a senior engineer.

  - id: researcher
    name: Research Agent
    model:
      provider: anthropic
      model: claude-sonnet-4-6
      api_key: ${ANTHROPIC_API_KEY}
```

- `dev` inherits provider, api_key, storage, and context; overrides model and system_prompt.
- `researcher` overrides provider, model, and api_key; inherits storage and context.

## Supported providers

| Provider | Description |
|----------|-------------|
| `openai` | OpenAI GPT-4o, GPT-4, GPT-3.5-turbo, o1, o3 |
| `anthropic` | Claude models |
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
