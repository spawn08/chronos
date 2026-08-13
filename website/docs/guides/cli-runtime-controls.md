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

Headless runs honor the same setting. CLI flags take precedence:

```bash
chronos run --stream "explain this code"     # force token streaming
chronos run --no-stream "explain this code"  # force one completed response
```

Inside the REPL, `/stream on` and `/stream off` change the current session. Switching agents applies that agent's explicit stream preference.

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
| Anthropic | extended `thinking` with `budget_tokens` |
| Gemini | `thinkingConfig` with budget and `includeThoughts` |

Reasoning is carried separately from final answer text in `ChatResponse.Reasoning`. The CLI displays streamed provider reasoning on stderr only when `summary: true`; normal answer text remains on stdout. Provider support varies by model and endpoint.

Anthropic and Gemini native reasoning is currently rejected when the request also contains tools because those providers require signed thought blocks to be preserved across tool rounds. Chronos fails closed instead of sending an invalid follow-up request. Prompt-based `cot`/`reflection` remains available with tools; OpenAI-compatible native reasoning with tools is unaffected.

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
- `--trace` attaches the storage-backed tracer and persists model, tool, and graph spans in the configured storage.
- Streaming and blocking model calls emit the same model-call hooks and tracing spans.

Environment equivalents:

```bash
CHRONOS_PERMISSION_MODE=auto_approve
CHRONOS_DEBUG=true
CHRONOS_TRACE=true
```

## Precedence

For process-wide runtime controls, explicit CLI flags/environment overrides take precedence over YAML. Per-tool `permission: deny` is always enforced, including in auto-approve mode.
