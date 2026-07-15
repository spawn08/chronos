# Operating Chronos from the CLI

A walkthrough of the Chronos CLI's **operational** commands — serving the control
plane, watching it live, managing the database, and inspecting/replaying
sessions. For building and chatting with agents from YAML, see
[`../cli_agent`](../cli_agent); this example focuses on running and observing a
deployment.

Every command is a real CLI verb — run `go run ./cli/main.go help` for the full
list.

## Quick offline check

These commands touch no network or model API, so they work immediately. A
ready-made script runs them end to end:

```bash
./examples/cli_ops/run.sh
```

It exercises `version`, `config show`, `db init`, `db status`, and
`sessions list` against a throwaway database.

## 1. Initialize and inspect the database

Chronos stores sessions, checkpoints, memory, traces, and events in SQLite by
default (path from `CHRONOS_DB_PATH`, default `chronos.db`).

```bash
export CHRONOS_DB_PATH=chronos.db
go run ./cli/main.go db init      # create + migrate the schema
go run ./cli/main.go db status    # path, size, modified time, session count
```

## 2. Serve the control plane

`serve` starts the ChronosOS HTTP control plane (default `:8420`) exposing
`/health`, `/api/sessions`, and `/metrics`.

```bash
go run ./cli/main.go serve :8420
```

Leave it running in one terminal.

## 3. Watch it live with the monitor

In a second terminal, `monitor` polls the control plane and renders a live
terminal dashboard (sessions, tool/model call counts, tokens, error rate, and
average model latency computed from the histogram).

```bash
go run ./cli/main.go monitor --endpoint http://localhost:8420 --interval 2
```

- `--endpoint`/`-e` — control-plane URL (an explicit flag overrides the
  `CHRONOS_ENDPOINT` env var).
- `--interval`/`-i` — refresh seconds (must be a positive integer).

Press `Ctrl+C` to exit. Unknown flags and invalid intervals are reported as
errors rather than silently ignored.

## 4. Inspect and replay sessions

Once agents have run (e.g. via `chronos run` or the REPL), inspect their durable
sessions:

```bash
go run ./cli/main.go sessions list                 # recent sessions
go run ./cli/main.go sessions export <session_id>  # markdown dump of the event log
go run ./cli/main.go sessions resume <session_id>  # resume a paused/running session from its latest checkpoint
```

`resume` reloads the agent from config, restores the latest checkpoint, and
continues execution — the durable-execution guarantee in action.

## 5. Batch processing with `pipe`

`pipe` reads one message per line from stdin and writes one JSON result per line
to stdout — ideal for scripting and pipelines. It needs a configured agent and
an API key.

```bash
export CHRONOS_CONFIG=examples/cli_agent/agents.yaml
export OPENAI_API_KEY=sk-...
printf 'What is 2+2?\nName the largest planet.\n' \
  | go run ./cli/main.go pipe assistant \
  | jq .
```

Each output line looks like `{"agent":"assistant","content":"..."}`; a failed
line emits `{"error":"..."}` and processing continues.

## 6. Deploy agents into a sandbox

`deploy` builds agents/teams from a deploy config and runs them with
sandbox-backed shell/file tools. A ready-made config lives in `../yaml-configs`:

```bash
export OPENAI_API_KEY=sk-...
go run ./cli/main.go deploy examples/yaml-configs/sandbox-deploy.yaml \
  "Build a REST API for todo items"
```

With no `teams:` in the config, `deploy` runs the **first agent in config
order** (deterministic); with a team, it runs the first team.

## Environment variables

| Variable            | Purpose                                                     |
|---------------------|-------------------------------------------------------------|
| `CHRONOS_DB_PATH`   | SQLite database path (default `chronos.db`).                |
| `CHRONOS_CONFIG`    | Path to the agents YAML config (for `pipe`, `run`, `repl`). |
| `CHRONOS_ENDPOINT`  | Default monitor endpoint (overridden by `--endpoint`).      |
| `OPENAI_API_KEY`    | Model API key consumed by `${OPENAI_API_KEY}` in configs.   |
