---
title: "YAML Agents & the Dashboard"
sidebar_label: "YAML Agents & Dashboard"
---

An `AgentConfig` in YAML can declare a real, durable `StateGraph` — nodes, edges,
and human-in-the-loop interrupts — with no Go code. Mark that agent
`durable: true` and `chronos serve` registers its compiled graph with ChronosOS
automatically, so its runs show up in [the dashboard](/guides/dashboard) exactly
like a graph built by hand in Go.

Before this existed, a plain YAML chat agent was invisible to ChronosOS: `Agent.Run`
only creates a session and checkpoints when the agent has a graph, and nothing
loaded YAML into `chronos serve` at all. Declaring a `graph:` block is what makes an
agent durable; `durable: true` is what tells `chronos serve` to expose it.

## A minimal example

```yaml
agents:
  - id: expense-approver
    name: Expense Approver
    model:
      provider: ollama
      model: llama3.3
    storage:
      backend: sqlite
      dsn: chronos.db
    durable: true
    graph:
      entry: prepare
      finish: disburse
      nodes:
        - id: prepare
          type: passthrough
          set:
            request_id: REQ-2026-0714
            status: pending_approval
        - id: gate
          type: passthrough
          interrupt: true
          set:
            approved: true
            status: approved
        - id: disburse
          type: passthrough
          set:
            status: disbursed
      edges:
        - from: prepare
          to: gate
        - from: gate
          to: disburse
```

Run it with zero application code:

```bash
chronos -c agent.yaml serve :8420
```

Then start a run, watch it pause at `gate`, and resume it — from curl or from
`/dashboard/`:

```bash
curl -X POST http://localhost:8420/api/dashboard/runs \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "expense-approver"}'

curl -X POST http://localhost:8420/api/dashboard/resume \
  -H 'Content-Type: application/json' \
  -d '{"session_id": "<the session_id from the previous response>"}'
```

A complete runnable copy of this example lives in
[`examples/yaml_dashboard/`](https://github.com/spawn08/chronos/tree/main/examples/yaml_dashboard):

```bash
go run ./examples/yaml_dashboard/
```

## `durable` vs. `graph`

These are two independent switches:

- **`graph:`** is what makes an agent's `Run` durable at all — once compiled, it's
  assigned to `Agent.Graph`, and `Agent.Run`/`Agent.Resume` behave exactly as they
  do for a Go-built graph (session creation, checkpointing, pause/resume). This
  works with or without `durable: true`, in any Go program that calls
  `agent.BuildAgent`/`BuildAll` — including your own `chronos deploy` or embedded
  service.
- **`durable: true`** additionally tells `chronos serve` to register that
  compiled graph with ChronosOS (a `dashboard.GraphRegistry` entry keyed by agent
  id), so `/api/dashboard/*` and the dashboard UI can see and drive its sessions.
  It requires `graph:` to be set, and requires `storage.backend` to be something
  other than `none`/`memory` (a durable agent needs somewhere to persist
  sessions).

## Node types

YAML can't carry an arbitrary Go closure, so each node picks one of four
declarative kinds:

| `type` | Behavior | Required fields |
|--------|----------|------------------|
| `model` | Renders `prompt` as a Go `text/template` against `{{.state.<key>}}`, calls the agent's model, writes the reply to `output_key` (default `response`). | `prompt` |
| `tool` | Calls a tool already registered under the agent's `tools:` list, passing `state[input_key]` (or the whole state when `input_key` is empty) as arguments, writing the result to `output_key` (default `<id>_result`). | `tool` |
| `subagent` | Delegates to another agent declared in the same file, calling its `Chat` with `state[input_key]` (or `state["message"]`), writing the reply to `output_key` (default `<id>_response`). | `agent` |
| `passthrough` | Merges a static `set:` map into state. The building block for an `interrupt: true` gate that just needs to record a decision on resume. | — |

Any node can set `interrupt: true` — the runner pauses and checkpoints *before*
executing that node, the same way `graph.AddInterruptNode` works in Go; the
node's own logic only runs once the run is resumed.

### `model` node

```yaml
nodes:
  - id: classify
    type: model
    prompt: |
      Classify this support ticket as "billing", "technical", or "general":
      {{.state.ticket}}
    output_key: category
```

### `tool` node

```yaml
tools:
  - name: send_email
    description: Sends an email notification
graph:
  nodes:
    - id: notify
      type: tool
      tool: send_email
      input_key: notification
      output_key: notify_result
```

A custom tool with no built-in implementation needs a handler wired via
`agent.WithToolHandler` when you call `BuildAgent`/`BuildAll` yourself (see
[Configuration](/getting-started/configuration)) — `chronos serve` alone cannot
supply one, since a handler is Go code, not YAML.

### `subagent` node

```yaml
agents:
  - id: researcher
    name: Researcher
    model: { provider: openai, model: gpt-4o }
  - id: writer
    name: Writer
    model: { provider: openai, model: gpt-4o }
    durable: true
    storage: { backend: sqlite, dsn: writer.db }
    graph:
      entry: research
      finish: research
      nodes:
        - id: research
          type: subagent
          agent: researcher
          output_key: findings
```

A `subagent` node can reference any agent defined anywhere in the same file —
`BuildAll` compiles every agent's graph only after every agent in the file has
been built, so ordering doesn't matter.

## Edges

A static edge is `{from, to}`. A conditional edge routes on
`state[route_key]`:

```yaml
edges:
  - from: classify
    conditional: true
    route_key: category
    routes:
      billing: billing_handler
      technical: tech_handler
    default: general_handler
```

`to`, a `routes` value, or `default` may be the literal `"__end__"` to route
straight to the end of the run without a trailing no-op node.

## See also

- [The Dashboard](/guides/dashboard) — the visual graph debugger this feeds
- [The ChronosOS Server](/guides/server) — `chronos serve`, auth, and `chronos auth token`
- [Durable Execution](/guides/durable-execution) — checkpoints, resume, and time-travel at the `engine/graph` level
