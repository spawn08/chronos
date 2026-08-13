---
title: "CLI Runtime Controls"
---

Chronos CLI runs can configure streaming, tool approvals, model reasoning, debug logs, and persisted traces from YAML or process-wide flags. Run `chronos -c agents.yaml config validate` to catch unknown YAML fields and invalid runtime policy before execution.

## Tool permission modes

Every tool keeps its declared permission (`allow`, `require_approval`, or `deny`). The registry permission mode controls what happens when an approval-gated tool is reached:

| Mode | Behavior |
|------|----------|
| `prompt` | Ask before each approval-gated tool (default) |
| `auto_approve` | Skip approval and confirmation prompts; explicit `deny` still wins |
| `deny` | Reject approval-gated tools without prompting |

Configure an agent in YAML:

```yaml
agents:
  - id: local-dev
    name: Local Developer
    permission_mode: auto_approve
    tools:
      - name: file_read
        permission: allow
      - name: file_write
        permission: require_approval
      - name: shell
        permission: deny
```

Override YAML for one CLI process:

```bash
chronos --permission-mode prompt repl
chronos --permission-mode auto_approve run "update the changelog"
chronos --permission-mode deny run "inspect this repository"
chronos --debug --trace run "diagnose this run"
chronos --no-debug --no-trace run "run quietly"

# Explicit shortcut for a trusted local environment:
chronos --dangerously-skip-permissions repl
```

`--dangerously-skip-permissions` is an alias for `--permission-mode auto_approve`. It does **not** override a tool declared with `permission: deny`.

### Approve the rest of an interactive session

At a CLI approval prompt, enter:

```text
Approve? [y/N/a=all for session]: a
```

The current call is approved and all later approval-gated tools are auto-approved for the rest of that CLI session. Enter `y` to approve only the current call, or anything else to deny it.

`chronos pipe` cannot safely prompt on the same stdin that supplies batch messages. It therefore changes the default `prompt` mode to `deny`. Select `auto_approve` explicitly in YAML, with `CHRONOS_PERMISSION_MODE`, or with a CLI flag when trusted tools must run in pipe mode.

## Streaming

The REPL streams by default unless the active agent has an explicit `stream` setting:

```yaml
agents:
  - id: assistant
    name: Assistant
    stream: true
```

Headless runs honor the same setting. A team run streams by default when every participating agent (including a separate coordinator) explicitly resolves to `stream: true`; mixed or unspecified team preferences default to a completed response. CLI flags take precedence:

```bash
chronos run --stream "explain this code"     # force token streaming
chronos run --no-stream "explain this code"  # force one completed response
chronos team run --stream pipeline "run it"  # force team token streaming
chronos team run --no-stream pipeline "run it"
```

Inside the REPL, `/stream on` and `/stream off` change the current session. Switching agents applies that agent's explicit stream preference.

Streaming emits model answer tokens as the provider produces them. Tool-only rounds may produce no answer text, so a tool-heavy agent can appear quiet while it reads or writes files. Enable `debug: true` or `--debug` to see live model-round and tool-call progress on stderr. A streaming transport failure is reported directly; it is never silently retried as a blocking call.

## Native reasoning and thinking

Reasoning has two independent controls:

- `strategy` adds Chronos prompt scaffolding: `none`, `cot`, or `reflection`.
- `native` asks a supported provider to use its native reasoning/thinking feature. When false, effort, budget, and summary settings are not sent to the provider.

```yaml
agents:
  - id: reasoner
    name: Reasoning Agent
    model:
      provider: anthropic
      model: claude-sonnet-4-6
      api_key: ${ANTHROPIC_API_KEY}
    reasoning:
      strategy: none
      native: true
      effort: high
      budget_tokens: 4096
      summary: true
```

Provider mapping:

| Provider | Native mapping |
|----------|----------------|
| OpenAI / compatible | `reasoning_effort` when `effort` is set |
| Azure OpenAI | Native reasoning uses `/openai/v1/responses`; encrypted reasoning items are preserved across tool rounds |
| Anthropic | extended `thinking` with `budget_tokens` |
| Gemini | `thinkingConfig` with budget and `includeThoughts` |

Reasoning is carried separately from final answer text in `ChatResponse.Reasoning`. The CLI displays provider-approved reasoning summaries on stderr only when both `native: true` and `summary: true`; normal answer text remains on stdout. Providers can legitimately return no summary for simple or tool-only rounds. Setting `native: false` disables `effort`, `budget_tokens`, and native summary output. Prompt strategies (`cot` and `reflection`) modify answer content and are not native reasoning summaries.

Anthropic and Gemini native reasoning is currently rejected when the request also contains tools because those providers require signed thought blocks to be preserved across tool rounds. Chronos fails closed instead of sending an invalid follow-up request. Azure OpenAI native reasoning with tools is sent through the Responses API and preserves encrypted reasoning state between rounds.

## Debug logs and traces

Enable runtime diagnostics in YAML:

```yaml
agents:
  - id: observed
    name: Observed Agent
    debug: true
    tracing: true
    storage:
      backend: sqlite
      dsn: chronos.db
```

Or override for one CLI process:

```bash
chronos --debug --trace run --stream "diagnose this failure"
```

- `--debug` writes detailed agent execution logs to stderr.
- `--trace` attaches the storage-backed tracer and persists model, tool, and graph spans in the configured storage; it does **not** print spans to the terminal.
- Tracing requires `storage.backend: sqlite` or `storage.backend: postgres`. Configuration fails clearly when tracing is combined with `none`/`memory` storage instead of silently dropping spans.
- A relative SQLite DSN such as `chronos.db` is resolved from the process working directory, not from the YAML file's directory. `chronos agent show <id>` prints the resolved absolute path.
- Streaming and blocking model calls emit the same model-call hooks and tracing spans. Span completion updates the original row with `ended_at`, output, and error data.

Environment equivalents:

```bash
CHRONOS_PERMISSION_MODE=auto_approve
CHRONOS_DEBUG=true             # false explicitly disables YAML debug
CHRONOS_TRACE=true             # false explicitly disables YAML tracing
```

## Precedence

For process-wide runtime controls, explicit CLI flags/environment overrides take precedence over YAML. Per-tool `permission: deny` is always enforced, including in auto-approve mode.
