---
title: "YAML CLI & Team Reference"
sidebar_label: "CLI & team reference"
---

Use this page while running the YAML examples.

## Validate first

```bash
chronos -c my-agents.yaml config validate
```

Validation catches unknown fields, missing model providers, invalid permission/reasoning values, duplicate IDs, and unknown team members before a model call starts.

## Agent commands

```bash
chronos -c my-agents.yaml agent list
chronos -c my-agents.yaml agent show <agent-id>
chronos -c my-agents.yaml agent chat <agent-id>

# One-shot message to the first configured agent.
chronos -c my-agents.yaml run "your message"

# Select an agent explicitly.
chronos -c my-agents.yaml run --agent <agent-id> "your message"
```

## Streaming controls

```bash
# Force live token output.
chronos -c my-agents.yaml run --stream --agent <agent-id> "your message"

# Force one completed response.
chronos -c my-agents.yaml run --no-stream --agent <agent-id> "your message"

# Stream a multi-agent team with agent labels.
chronos -c my-agents.yaml team run --stream <team-id> "your task"
```

Inside the REPL:

```text
/stream       # show current state
/stream on
/stream off
```

An explicit agent-level `stream: true|false` is the default when neither command flag is supplied.

## Tool permission controls

```bash
# Prompt according to each tool's permission (default).
chronos --permission-mode prompt -c agents.yaml repl

# Reject approval-gated calls without prompting.
chronos --permission-mode deny -c agents.yaml run "read-only review"

# Trusted local/disposable environment only.
chronos --permission-mode auto_approve -c agents.yaml run "implement the task"
chronos --dangerously-skip-permissions -c agents.yaml repl
```

At an interactive prompt, enter `a` to auto-approve approval-gated tools for the remainder of that CLI session. A tool with `permission: deny` is never bypassed.

`chronos pipe` changes the default prompt mode to `deny` because batch input and approval responses cannot safely share stdin. Select a non-interactive mode explicitly when tools are required.

## Debug and trace controls

```bash
chronos --debug --trace -c agents.yaml run --stream "diagnose this task"
```

Environment equivalents:

```bash
export CHRONOS_PERMISSION_MODE=prompt
export CHRONOS_DEBUG=true
export CHRONOS_TRACE=true
```

## Team commands

```bash
chronos -c agents.yaml team list
chronos -c agents.yaml team show <team-id>
chronos -c agents.yaml team run <team-id> "task description"
chronos -c agents.yaml team run --stream <team-id> "task description"
```

## Team strategies

| Strategy | How it works | Best for |
|----------|--------------|----------|
| `sequential` | Runs every agent in order; output flows forward | Research → write → edit |
| `parallel` | Runs agents concurrently on the same input | Comparisons and review panels |
| `router` | Selects exactly one agent | Intent-based support dispatch |
| `coordinator` | Supervisor plans, delegates, and reviews | Complex projects |
| `swarm` | Peers hand off ownership dynamically | Incident response, investigation |
| `hierarchy` | Root supervisor delegates to workers | Organization-style planning |

### Team fields

| Field | Purpose |
|-------|---------|
| `id`, `name` | Team identity |
| `strategy` | One of the six strategies above |
| `agents` | Member agent IDs; order matters for sequential teams |
| `coordinator` | Coordinator agent or hierarchy root |
| `router` | `model` or `capability` |
| `router_model` | Optional cheaper/faster model for routing |
| `initial_agent` | Starting peer for a swarm |
| `max_handoffs` | Swarm hand-off bound |
| `max_concurrency` | Parallel worker bound |
| `max_iterations` | Coordinator planning bound |
| `error_strategy` | `fail_fast`, `collect`, or `best_effort` |

## Config file selection

Resolution order:

1. `-c` / `--config`
2. `CHRONOS_CONFIG`
3. `./.chronos/agents.yaml` or `.yml`
4. `./agents.yaml` or `.yml`
5. `~/.chronos/agents.yaml` or `.yml`

```bash
chronos -c /path/to/config.yaml team run my-team "do something"
chronos team show my-team --config content-pipeline.yaml

export CHRONOS_CONFIG=/path/to/config.yaml
chronos team run my-team "do something"
```

## Bundled configurations

```bash
export OPENAI_API_KEY=sk-your-key-here

# Simple agent and sequential review team.
chronos -c examples/cli_agent/agents.yaml config validate

# Router.
chronos -c examples/yaml-configs/customer-support.yaml \
  team run support "I need a refund for order #12345"

# Sequential pipeline.
chronos -c examples/yaml-configs/content-pipeline.yaml \
  team run pipeline "Write about renewable energy trends"

# Coordinator.
chronos -c examples/yaml-configs/coding-team.yaml \
  team run dev-team "Build a REST API for user management"

# Multi-provider parallel team.
chronos -c examples/yaml-configs/multi-provider.yaml \
  team run compare "What is the meaning of life?"

# Sandboxed deployment schema.
chronos deploy examples/yaml-configs/sandbox-deploy.yaml \
  "Build a REST API for todo items"
```

## Design tips

- Start with one agent; add teams only when roles or execution order are genuinely different.
- Put provider and storage repetition in `defaults`.
- Keep one application or workflow per YAML file.
- Write precise `description` and `capabilities` values for routers and coordinators.
- Use `storage: {backend: none}` for stateless team workers.
- Bound concurrency, iterations, hand-offs, context, and command timeouts.
- Default write, shell, SQL, and network tools to approval or deny.
- Validate configs and run evaluation gates in CI.
