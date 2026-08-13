---
title: "4. Production: Governed Applications"
sidebar_label: "4. Production applications"
---

Production-oriented agents need more than prompts: tool policy, explicit approvals, context limits, reasoning controls, persistent traces, and isolation for autonomous actions.

## Governed repository agent

This agent can inspect and modify a repository, but read operations are automatic while writes and shell commands require approval.

### Create `governed-coder.yaml`

```yaml
agents:
  - id: governed-coder
    name: Governed Coding Agent
    description: Investigates and implements repository changes under explicit tool policy

    model:
      provider: openai
      model: gpt-5.5
      api_key: ${OPENAI_API_KEY}
      timeout_sec: 120

    storage:
      backend: sqlite
      dsn: .chronos/governed-coder.db

    system_prompt: |
      You are a senior software engineer working in an existing repository.

      Operating rules:
      1. Inspect relevant files before proposing changes
      2. Make the smallest coherent implementation
      3. Run focused tests, then the full validation command
      4. Never claim a command passed unless its output confirms success
      5. Summarize changed files and verification evidence

    instructions:
      - Preserve existing public APIs unless the task requires a breaking change.
      - Do not access paths outside the current working directory.

    stream: true
    debug: false
    tracing: true
    permission_mode: prompt
    num_history_runs: 3

    reasoning:
      strategy: none
      native: true
      effort: high
      budget_tokens: 4096
      summary: false

    context:
      max_tokens: 100000
      summarize_threshold: 0.8
      preserve_recent_turns: 6

    tools:
      - name: file_read
        permission: allow
      - name: file_list
        permission: allow
      - name: file_glob
        permission: allow
      - name: file_grep
        permission: allow
      - name: file_write
        permission: require_approval
      - name: shell
        permission: require_approval
```

:::note Native reasoning with tools
OpenAI-compatible reasoning can be combined with tools. Anthropic and Gemini native reasoning currently fails closed when tools are present because those providers require signed thought blocks across tool rounds. For those providers, use `native: false` with `strategy: reflection`, or remove tools.
:::

### Run safely

```bash
mkdir -p .chronos
export OPENAI_API_KEY=sk-your-key-here

chronos -c governed-coder.yaml config validate
chronos -c governed-coder.yaml run --stream --agent governed-coder \
  "Add table-driven tests for the parser package"
```

At an approval prompt:

```text
Approve? [y/N/a=all for session]:
```

- Enter `y` to approve only that call.
- Enter `a` to auto-approve later approval-gated tools for this CLI session.
- Enter anything else to deny.

For read-only analysis, force every approval-gated action to fail:

```bash
chronos --permission-mode deny -c governed-coder.yaml run \
  --agent governed-coder "Review this repository without changing it"
```

For a trusted disposable environment only:

```bash
chronos --dangerously-skip-permissions -c governed-coder.yaml run \
  --agent governed-coder "Implement and test the requested change"
```

An explicit `permission: deny` always remains blocked, even in auto-approve mode.

## Production storage profile

Replace local SQLite with PostgreSQL when multiple replicas share sessions and traces:

```yaml
storage:
  backend: postgres
  dsn: ${CHRONOS_STORAGE_DSN}
  max_open_conns: 30
  max_idle_conns: 10
  conn_max_lifetime_sec: 1800
```

```bash
export CHRONOS_STORAGE_DSN='postgres://chronos:secret@db:5432/chronos?sslmode=require'
chronos -c governed-coder.yaml config validate
```

Keep credentials in environment variables or a secret manager—not in YAML committed to source control.

## Sandboxed autonomous build team

For broader autonomy, use `chronos deploy` with the deployment YAML schema. The sandbox limits command duration and working directory while a planner, coder, and QA agent work sequentially.

### Create `sandbox-deploy.yaml`

```yaml
name: sandbox-coding-team

sandbox:
  backend: process
  work_dir: /tmp/chronos-sandbox
  timeout: 5m

defaults:
  model:
    provider: openai
    api_key: ${OPENAI_API_KEY}
    model: gpt-4o
  storage:
    backend: none

agents:
  - id: planner
    name: Task Planner
    description: Analyzes requirements and creates an implementation plan
    system_prompt: |
      Break the task into clear steps, identify files to change,
      and define acceptance criteria before implementation.
    capabilities: [planning, analysis]
    tools:
      - name: file_list
      - name: file_read
      - name: file_grep

  - id: coder
    name: Code Implementer
    description: Implements the approved plan in the sandbox
    system_prompt: |
      Read existing files before modifying them. Write production-quality code,
      handle errors, and run the build after changes.
    capabilities: [implementation, coding]
    tools:
      - name: file_read
      - name: file_write
      - name: file_list
      - name: file_glob
      - name: file_grep
      - name: shell_auto

  - id: qa
    name: QA Engineer
    description: Tests the implementation and reports verification evidence
    system_prompt: |
      Run focused and full tests, check edge cases, and report exact failures.
    capabilities: [testing, verification]
    tools:
      - name: file_read
      - name: file_write
      - name: shell_auto

teams:
  - id: build-team
    name: Build Team
    strategy: sequential
    agents: [planner, coder, qa]
```

### Deploy and run

```bash
export OPENAI_API_KEY=sk-your-key-here
chronos deploy sandbox-deploy.yaml \
  "Build a Go REST API for todo items with tests"
```

:::warning Sandbox boundary
`shell_auto` skips interactive approval because execution is expected to occur inside the configured sandbox. Prefer the Kubernetes or WASM sandbox for stronger isolation of untrusted workloads; process isolation is a development convenience, not a complete security boundary.
:::

The repository includes the full runnable file at [`examples/yaml-configs/sandbox-deploy.yaml`](https://github.com/spawn08/chronos/blob/main/examples/yaml-configs/sandbox-deploy.yaml).

## MCP-enriched agents

YAML can declare MCP servers:

```yaml
mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]
```

After loading YAML through the Go SDK, call `ConnectMCP(ctx)` to connect and import tools. The current CLI YAML loader does not connect MCP servers automatically. See the [MCP guide](/guides/mcp/) for the complete lifecycle and permission handling.

## Production checklist

- [ ] `chronos config validate` succeeds in CI.
- [ ] Write/network/shell tools are `require_approval` or `deny` by default.
- [ ] Non-interactive jobs select `deny` or `auto_approve` explicitly.
- [ ] Tracing is enabled with durable storage.
- [ ] Secrets come from environment variables or a secret manager.
- [ ] Context budgets and iteration limits are bounded.
- [ ] Autonomous shell/file execution is sandboxed.
- [ ] Evaluation suites gate behavioral regressions.

Continue with [CLI and team reference](./cli-reference) or the full [deployment guides](/deployment/docker/).
