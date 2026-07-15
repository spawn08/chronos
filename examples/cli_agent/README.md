# Building an Agent with the Chronos CLI

A step-by-step walkthrough of building, inspecting, and running an agent
**entirely from the Chronos CLI** — driven by a YAML config, no application code
required for the basics. It also shows the supported way to give an agent a
**custom tool with real behavior** (a small Go program), since custom tools
declared in YAML are placeholders.

Every command below uses the actual Chronos CLI verbs
(`go run ./cli/main.go <command>`). Run `go run ./cli/main.go help` to see them.

## Files

| File             | Purpose                                                        |
|------------------|----------------------------------------------------------------|
| `agents.yaml`    | Declarative agent + team config the CLI loads.                 |
| `custom_tool.go` | A Go program that registers a **real** custom tool handler.    |

## 1. Point the CLI at the config

The CLI looks for a config in this order: `$CHRONOS_CONFIG`, then
`.chronos/agents.yaml`, `agents.yaml`, and finally `~/.chronos/agents.yaml`.

Use the environment variable to load this example's config:

```bash
export CHRONOS_CONFIG=examples/cli_agent/agents.yaml
```

Or place it at the project-default location instead of setting the variable:

```bash
mkdir -p .chronos
cp examples/cli_agent/agents.yaml .chronos/agents.yaml
```

The config reads the model API key from the environment via `${OPENAI_API_KEY}`
expansion, so export your key before any command that actually talks to the
model (`agent chat`, `run`, `repl`, `team run`):

```bash
export OPENAI_API_KEY=sk-...
```

Commands that only read the config (`agent list`, `agent show`, `team list`,
`team show`) work without a key.

## 2. List the agents in the config

```bash
CHRONOS_CONFIG=examples/cli_agent/agents.yaml go run ./cli/main.go agent list
```

```
ID              NAME                 PROVIDER        MODEL           DESCRIPTION
-------------------------------------------------------------------------------------
assistant       CLI Assistant        openai          gpt-4o          A concise general-purpose a...
repo-explorer   Repository Explorer  openai          gpt-4o          Answers questions about the...
```

## 3. Inspect one agent

```bash
CHRONOS_CONFIG=examples/cli_agent/agents.yaml go run ./cli/main.go agent show repo-explorer
```

Shows the resolved provider, model, storage, system prompt, instructions, and
capabilities.

## 4. Run a one-shot message (headless)

`run` sends a single message and prints the response. Choose an agent with
`--agent` (alias `-a`); omit it to use the first agent in the config.

```bash
# default (first) agent
CHRONOS_CONFIG=examples/cli_agent/agents.yaml go run ./cli/main.go run "Give me one tip for writing clear commit messages"

# a specific agent
CHRONOS_CONFIG=examples/cli_agent/agents.yaml go run ./cli/main.go run --agent assistant "Summarize what a durable agent is in one sentence"
```

## 5. Chat interactively with one agent

```bash
CHRONOS_CONFIG=examples/cli_agent/agents.yaml go run ./cli/main.go agent chat assistant
```

Opens an interactive REPL bound to that agent. Type messages; the conversation
is persisted to the configured SQLite database.

## 6. Start the general REPL

`repl` starts the interactive shell and loads the first agent from the config:

```bash
CHRONOS_CONFIG=examples/cli_agent/agents.yaml go run ./cli/main.go repl
```

## 7. Serve the ChronosOS control plane

`serve` starts the HTTP control plane (default `:8420`):

```bash
CHRONOS_CONFIG=examples/cli_agent/agents.yaml go run ./cli/main.go serve :8420
```

## 8. Run the team

The config defines a sequential team named `review`:

```bash
CHRONOS_CONFIG=examples/cli_agent/agents.yaml go run ./cli/main.go team list
CHRONOS_CONFIG=examples/cli_agent/agents.yaml go run ./cli/main.go team show review
CHRONOS_CONFIG=examples/cli_agent/agents.yaml go run ./cli/main.go team run review "Summarize the README in this directory"
```

## Custom tools: YAML declares, Go implements

Chronos resolves **built-in** tool names in YAML to real handlers automatically
(`file_read`, `file_write`, `file_list`, `file_glob`, `file_grep`, `shell`,
`shell_auto`). The `repo-explorer` agent uses several of these.

A **custom** tool in YAML (a name + description that is not a built-in) is
registered as a **no-op placeholder**: the model can see the tool, but calling
it just echoes the arguments. In `agents.yaml` the `word_count` tool is such a
placeholder.

To give a custom tool real behavior, register a `tool.Definition` with a
`Handler` **programmatically in Go**. `custom_tool.go` shows the supported
pattern — it wires a real `word_count` handler onto the `repo-explorer` agent
and invokes it two ways (direct execution and a model-driven tool call). It uses
a deterministic mock provider, so it runs with no API key:

```bash
go run ./examples/cli_agent/
```

```
━━━ Direct tool execution ━━━
  word_count("the quick brown fox") = map[words:4]

━━━ Model-driven tool call ━━━
  Assistant: That text contains 7 words.
```

In production you would build the agent from `agents.yaml`
(`agent.BuildAgent`), then `AddTool` your real handler for any custom tool name
before serving it.

## Useful environment variables

| Variable          | Purpose                                             |
|-------------------|-----------------------------------------------------|
| `CHRONOS_CONFIG`  | Path to the agents YAML config file.                |
| `CHRONOS_DB_PATH` | SQLite database path (default `chronos.db`).        |
| `OPENAI_API_KEY`  | Consumed by `${OPENAI_API_KEY}` in `agents.yaml`.   |

## Other CLI commands worth knowing

```bash
go run ./cli/main.go help        # full command list
go run ./cli/main.go version     # build/version info
go run ./cli/main.go sessions    # session management (list/resume/export)
go run ./cli/main.go config      # show resolved configuration
```
