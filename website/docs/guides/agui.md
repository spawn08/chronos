---
title: "AG-UI Event Stream"
sidebar_label: "AG-UI Stream"
---


Frontends shouldn't need bespoke glue to render a Chronos run. The **AG-UI event
stream** translates Chronos's native execution events into the standard
[AG-UI](https://ag-ui.com) protocol, so any AG-UI-compatible frontend can render
a run — steps, tool calls, plan updates, state snapshots, and completion — out of
the box. It is served alongside the native `/api/events/stream`, which is
unchanged.

## Endpoint

```
GET /api/agui/stream?session=<id>&run=<id>
```

Server-Sent Events, one AG-UI event per `data:` frame. Scope the stream to a
session with `?session=<id>` (the AG-UI *thread*); omit it to subscribe to the
firehose (all sessions — useful for a dashboard). Per-session isolation, the
subscriber cap, and heartbeats are inherited from the [event broker](/guides/streaming).

## Event mapping

Each native broker event maps to zero or more AG-UI events:

| Chronos event | AG-UI event(s) |
|---------------|----------------|
| (on connect) | `RUN_STARTED` |
| `node_start` / `node_end` | `STEP_STARTED` / `STEP_FINISHED` |
| `model_delta` (streamed token) | `TEXT_MESSAGE_START` (first) → `TEXT_MESSAGE_CONTENT` |
| `model_response` | `TEXT_MESSAGE_END` (closes a streamed message), or a full `START`→`CONTENT`→`END` when non-streaming |
| `tool_call` | `TOOL_CALL_START` → `TOOL_CALL_ARGS` → `TOOL_CALL_END` |
| `tool_result` | `TOOL_CALL_RESULT` (a `TOOL_CALL_START` is synthesized first if this connection never saw the call) |
| `plan_update` ([planning tool](/guides/planning)) | `CUSTOM` (`name: "plan"`) |
| `checkpoint` | `STATE_SNAPSHOT` |
| `interrupt` (HITL) | `CUSTOM` (`name: "interrupt"`) |
| `completed` | `RUN_FINISHED` |
| `error` | `RUN_ERROR` |
| `custom` | `CUSTOM` |

`RUN_STARTED` is emitted immediately on connect (not lazily), so a client sees a
live, well-formed lifecycle even before the run produces its first event.
Assistant text streams as `TEXT_MESSAGE_*` (token by token on the streaming path,
or as one message on the blocking path). `TOOL_CALL_*` events correlate by the
model's tool-call id when present (falling back to a per-run synthetic id).

The agent routes its model/tool events to the session topic (via
`ChatWithSession` or a graph run), so a `?session=<id>` subscriber is genuinely
isolated from other sessions' events; the plain `Chat` method (no session)
broadcasts to the firehose.

## Embedding the handler

The mapping layer (`os/interop/agui`) is a thin translator over the
`stream.Broker`; you can mount it on your own server:

```go
import "github.com/spawn08/chronos/os/interop/agui"

http.Handle("/agui", agui.Handler(broker)) // broker is your *stream.Broker
```

The `agui.Translator` is also usable directly if you deliver events over a
transport other than SSE — `NewTranslator(thread, run)`, then `Start()` once and
`Translate(evt)` per broker event.

## Example

A complete, runnable example (no API key) is in
[`examples/agui_stream`](https://github.com/spawn08/chronos/tree/main/examples/agui_stream).
It replays a scripted run through the broker and prints the AG-UI events a
frontend would receive.
