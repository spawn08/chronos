---
title: "CLI Reference"
permalink: /api/cli/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

The Chronos CLI provides interactive and headless access to agents, sessions, memory, and the control plane server.

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

Start an interactive REPL session with the configured agent.

```bash
chronos repl
chronos repl --config .chronos/agents.yaml
```

The REPL loads the default agent from the config file. Non-command input is sent to the agent for chat.

### serve

Start the ChronosOS HTTP control plane server.

```bash
chronos serve :8420
chronos serve --port 9000
```

Exposes REST APIs for sessions, traces, approval, and SSE streaming.

### run

Execute a one-shot message in headless mode.

```bash
chronos run "explain Go interfaces"
chronos run --agent researcher "compare React vs Svelte"
```

### agent

Manage configured agents.

```bash
chronos agent list              # list all agents from config
chronos agent show dev          # show details for agent "dev"
chronos agent chat dev          # start chat with a specific agent
```

### sessions

Manage execution sessions.

```bash
chronos sessions list           # list past sessions
chronos sessions resume <id>    # resume a paused session
chronos sessions export <id>    # export session as markdown or JSON
```

### memory

Manage agent memory.

```bash
chronos memory list <agent_id>  # list stored memories
chronos memory forget <key>     # remove a memory entry
chronos memory clear            # clear all memories (with confirmation)
```

### db

Database management.

```bash
chronos db init                 # run storage migrations
chronos db status               # show connection and migration info
chronos db backup               # export a backup
```

### config

Configuration management.

```bash
chronos config show             # display loaded config
chronos config set key=value    # set a config value
chronos config model            # show/set default model
```

## REPL Slash Commands

When in the REPL, the following slash commands are available:

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/agent` | Show current agent info |
| `/model` | Show current model provider and model ID |
| `/sessions` | List recent sessions |
| `/memory` | List memories for the current agent |
| `/history` | Show conversation history |
| `/clear` | Clear conversation history |
| `/quit` | Exit the REPL |

### Shell Escape

Run shell commands from within the REPL using `!`:

```
dev> ! ls -la
dev> ! git status
```

### Multi-Line Input

Use triple quotes for multi-line input:

```
dev> """
Write a function that:
1. Accepts a list of integers
2. Returns the top 3
"""
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `CHRONOS_CONFIG` | Path to config file (overrides auto-discovery) |
| `OPENAI_API_KEY` | OpenAI API key |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `GEMINI_API_KEY` | Google Gemini API key |
| `MISTRAL_API_KEY` | Mistral API key |
| `STORAGE_DSN` | Storage connection string |
| `DATABASE_URL` | Alternative to STORAGE_DSN |

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
| `1` | General error |
| `2` | Configuration error |
