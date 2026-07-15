---
title: "CLI Reference"
---


The Chronos CLI provides interactive and headless access to agents, teams, sessions, memory, the durable control plane, and a live monitor.

## Installation

```bash
# Build from source
make build
# Binary available at bin/chronos

# Or run directly
go run ./cli/main.go help
```

## Commands

### repl

Start an interactive REPL. It loads the first agent from the config file (if any); non-command input is sent to that agent.

```bash
chronos repl
# point at a specific config via the environment:
CHRONOS_CONFIG=.chronos/agents.yaml chronos repl
```

### serve

Start the ChronosOS HTTP control plane server. The address is a positional argument (default `:8420`).

```bash
chronos serve            # listens on :8420
chronos serve :9000      # custom port
```

Exposes `/health`, `/api/sessions`, `/metrics`, and SSE streaming.

### run

Execute a one-shot message in headless mode. Select an agent with `--agent`/`-a`; omit it to use the first agent in the config.

```bash
chronos run "explain Go interfaces"
chronos run --agent researcher "compare React vs Svelte"
```

### pipe

Non-interactive batch mode: reads one message per line from stdin, writes one JSON result per line to stdout. An optional argument selects the agent.

```bash
printf 'What is 2+2?\nName the largest planet.\n' | chronos pipe assistant
# {"agent":"assistant","content":"..."}
```

A line that errors emits `{"error":"..."}` and processing continues.

### agent

Manage configured agents.

```bash
chronos agent list              # list all agents from config
chronos agent show dev          # show details for agent "dev"
chronos agent chat dev          # start an interactive chat with a specific agent
```

### team

Manage and run multi-agent teams.

```bash
chronos team list               # list teams from config
chronos team show review        # show a team's strategy, agents, coordinator
chronos team run review "..."   # run a team on a task
```

Supported strategies: `sequential`, `parallel`, `router`, `coordinator`, `swarm`, `hierarchy`.

### deploy

Build agents/teams from a deploy config and run them with sandbox-backed shell/file tools.

```bash
chronos deploy config.yaml "Build a REST API for todo items"
```

With a `teams:` section, the first team runs; otherwise the **first agent in config order** runs (deterministic).

### sessions

Manage durable execution sessions.

```bash
chronos sessions list           # list recent sessions
chronos sessions resume <id>    # resume a paused/running session from its latest checkpoint
chronos sessions export <id>    # export the session's event log as markdown
```

### memory

Manage agent long-term memory.

```bash
chronos memory list <agent_id>  # list stored memories
chronos memory forget <id>      # remove one memory entry by ID
chronos memory clear <agent_id> # clear all memories for an agent
```

### db

Database management.

```bash
chronos db init                 # create schema + run migrations
chronos db status               # path, size, modified time, session count
```

### eval

Evaluation suites.

```bash
chronos eval list               # list eval suite YAML files under .chronos/evals/ or evals/
chronos eval run suite.yaml     # load an eval suite
```

### monitor

Live terminal dashboard that polls the control plane (sessions, tool/model calls, tokens, error rate, average model latency).

```bash
chronos monitor
chronos monitor --endpoint http://localhost:8420 --interval 2
```

| Flag | Description |
|------|-------------|
| `--endpoint`, `-e` | Control-plane URL. An explicit flag overrides `CHRONOS_ENDPOINT`. |
| `--interval`, `-i` | Refresh interval in seconds (must be a positive integer). |

Invalid intervals and unknown flags are reported as errors. Press `Ctrl+C` to exit.

### config

Configuration management. `set` takes two positional arguments and persists to `~/.chronos/config.yaml`.

```bash
chronos config show             # display resolved config + loaded agents
chronos config set model gpt-5.5
chronos config model            # show/set the default model
```

### version

```bash
chronos version                 # version, commit, build date, Go version, os/arch
```

## REPL Slash Commands

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/agent` | Show current agent info (when an agent is loaded) |
| `/model` | Show current model provider and model ID (when an agent is loaded) |
| `/sessions` | List recent sessions |
| `/checkpoints <session_id>` | List checkpoints for a session |
| `/memory [agent_id]` | List memories (defaults to the loaded agent) |
| `/history` | Show conversation history for this REPL session |
| `/clear` | Clear conversation history |
| `/quit` | Exit the REPL |

### Shell Escape

Run a program from within the REPL using `!`. The command is split on whitespace and executed directly (no shell), so pipes, quotes, and `$VAR` expansion are not interpreted.

```
dev> ! ls -la
dev> ! git status
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `CHRONOS_CONFIG` | Path to the agents config file (overrides auto-discovery) |
| `CHRONOS_DB_PATH` | SQLite database path (default `chronos.db`) |
| `CHRONOS_ENDPOINT` | Default `monitor` endpoint (overridden by `--endpoint`) |
| `CHRONOS_MODEL` | Default model id shown/used by `config` |
| `CHRONOS_API_KEY` | Default model API key |
| `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` / `MISTRAL_API_KEY` | Provider API keys (expanded from `${VAR}` in configs) |

## Config File Discovery

The CLI searches for config files in this order:

1. Path specified by `CHRONOS_CONFIG`
2. `.chronos/agents.yaml` (project-level)
3. `.chronos/agents.yml`
4. `agents.yaml` (current directory)
5. `agents.yml`
6. `~/.chronos/agents.yaml` (global)
7. `~/.chronos/agents.yml`

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Any error (message printed to stderr) |

## See Also

- [`examples/cli_agent`](https://github.com/spawn08/chronos/tree/main/examples/cli_agent) — build and chat with agents from YAML.
- [`examples/cli_ops`](https://github.com/spawn08/chronos/tree/main/examples/cli_ops) — serve, monitor, db, sessions, pipe, deploy.
