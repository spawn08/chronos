---
title: "Streaming & SSE"
---


Chronos provides two streaming mechanisms: model-level token streaming via `StreamChat` and graph-level execution streaming via the `Runner` and SSE `Broker`.

## Model Streaming

Every provider supports streaming via `StreamChat`, which returns a channel of partial responses:

```go
ch, err := provider.StreamChat(ctx, &model.ChatRequest{
    Messages: []model.Message{
        {Role: "user", Content: "Tell me a story about a robot"},
    },
})
if err != nil {
    log.Fatal(err)
}

for chunk := range ch {
    fmt.Print(chunk.Content) // tokens arrive incrementally
}
fmt.Println() // final newline
```

Each `ChatResponse` on the channel has `Delta: true` to indicate it is a partial response. The channel is closed when generation is complete.

### Streaming with Tool Calls

Tool calls may arrive in chunks. Accumulate them:

```go
var toolCalls []model.ToolCall
for chunk := range ch {
    if len(chunk.ToolCalls) > 0 {
        toolCalls = append(toolCalls, chunk.ToolCalls...)
    }
    fmt.Print(chunk.Content)
}
```

## Agent Streaming

`StreamChat` is the low-level provider API. For most applications use the
agent-level `Agent.ChatStream`, which assembles the full prompt context (system
prompt, instructions, few-shot examples, long-term memory, RAG knowledge and run
history), handles the tool-calling loop, and streams the answer token by token —
mirroring the blocking `Agent.Chat` but incrementally.

```go
ch, err := myAgent.ChatStream(ctx, "Tell me a story about a robot")
if err != nil {
    log.Fatal(err) // e.g. no model configured, or an input guardrail rejected the message
}

var usage model.Usage
for chunk := range ch {
    switch {
    case chunk.Err != nil:
        log.Fatalf("stream failed: %v", chunk.Err) // terminal chunk
    case chunk.Delta:
        fmt.Print(chunk.Content) // live token fragment
    default:
        usage = chunk.Usage // final summary chunk (Delta == false)
    }
}
fmt.Printf("\n[tokens: %d + %d]\n", usage.PromptTokens, usage.CompletionTokens)
```

### Channel protocol

The returned channel yields three kinds of `*model.ChatResponse`, and is closed
when the turn finishes. Always drain it to completion.

| Chunk | Identify by | Meaning |
|-------|-------------|---------|
| Delta | `Delta == true` | A `Content` fragment to print as it arrives |
| Final | `Delta == false`, `Err == nil` | One trailing chunk carrying aggregated `Usage` and `StopReason` (empty `Content`) |
| Error | `Err != nil` | Fatal error; it is the last chunk on the channel |

Tool calls are handled transparently: when the model requests tools, the
fragments are aggregated, the tools run, and the follow-up turn is streamed — so
you see live text across every round.

:::note Guardrails & output schema
Because tokens are emitted as they arrive, **output guardrails and `OutputSchema`
are validated only after the full response has streamed**. A validation failure
surfaces as a trailing `Err` chunk — by which point the (rejected) text has
already been shown. Use the blocking `Agent.Chat` when you need pre-emission
validation. Input guardrails still run up front: an input rejection is returned
as the error from `ChatStream` itself, before any channel is produced.
:::

## CLI Streaming

The interactive REPL streams responses **by default**. Start it with:

```bash
chronos repl                 # uses the default agent from .chronos/agents.yaml
chronos agent chat <agent>   # chat with a specific agent
```

Tokens print as the model generates them. Toggle streaming at runtime with the
`/stream` slash command:

```
agent> /stream off      # switch to blocking (whole-response) output
Streaming is off.
agent> /stream on       # back to token-by-token
Streaming is on.
agent> /stream          # show current state
Streaming is on.
```

For one-shot headless runs, opt in with the `--stream` (`-s`) flag:

```bash
chronos run --stream "Summarize the latest release notes"
chronos run --agent support-bot -s "Draft a reply to ticket 4821"
```

Without `--stream`, `chronos run` prints the full response once it completes.

Multi-agent teams accept the same flag. Each agent's output is printed under a
labeled header so you can tell whose tokens are whose:

```bash
chronos team run --stream pipeline "what can you do?"
```

```
─── researcher ───
Searching the knowledge base…
─── writer ───
Here is a summary of what the team can do…
```

For the `parallel` strategy, tokens from different agents interleave; the header
is reprinted whenever the active agent changes.

## Team Streaming

Multi-agent teams stream too, via `Team.RunStream`, which returns a channel of
`TeamStreamEvent`. Each token is tagged with the `AgentID` that produced it — so
you can attribute output even when agents run concurrently.

```go
ch, err := myTeam.RunStream(ctx, graph.State{"message": "what can you do?"})
if err != nil {
    log.Fatal(err)
}

for evt := range ch {
    switch evt.Type {
    case team.TeamEventAgentStart:
        fmt.Printf("\n─── %s ───\n", evt.AgentID)
    case team.TeamEventToken:
        fmt.Print(evt.Content) // live token from evt.AgentID
    case team.TeamEventAgentEnd:
        fmt.Println()
    case team.TeamEventError:
        log.Fatalf("team failed: %v", evt.Err)
    case team.TeamEventComplete:
        // evt.State holds the merged final state
    }
}
```

The stream always ends with exactly one terminal event: `TeamEventComplete`
(carrying the merged final `State`) on success, or `TeamEventError` on failure.

### Strategy support

| Strategy | Streams tokens? |
|----------|-----------------|
| `sequential` | ✅ agents stream in pipeline order |
| `parallel` | ✅ tokens interleave; use `AgentID` to attribute |
| `router` | ✅ the selected agent streams |
| `coordinator` | ✅ delegated task agents stream (planning steps do not) |
| `hierarchy` | ✅ supervisors and workers stream |
| `swarm` | ⚠️ runs to completion but does **not** emit tokens — swarm inspects tool-call output to route handoffs, which is incompatible with token streaming |

The same guardrail/schema timing caveat as [Agent Streaming](#agent-streaming)
applies to each agent's output.

## Graph Execution Streaming

The `Runner` emits `StreamEvent` values as nodes execute:

```go
runner := graph.NewRunner(compiled, store)

// Start consuming events before Run
go func() {
    for evt := range runner.Stream() {
        fmt.Printf("[%s] node=%s\n", evt.Type, evt.NodeID)
    }
}()

result, err := runner.Run(ctx, sessionID, initialState)
```

### Event Types

| Type | When |
|------|------|
| `node_start` | Before a node function executes |
| `node_end` | After a node function completes |
| `edge_transition` | When the runner moves to the next node |
| `interrupt` | When an interrupt node pauses execution |
| `error` | When a node returns an error |
| `completed` | When the graph reaches its finish point |

### StreamEvent Structure

```go
type StreamEvent struct {
    Type      string
    NodeID    string
    State     State
    Error     string
    Timestamp time.Time
}
```

## SSE Broker

The `stream.Broker` provides server-sent events for web clients:

```go
import "github.com/spawn08/chronos/engine/stream"

broker := stream.NewBroker()

// Subscribe a client
ch := broker.Subscribe("client-123")
defer broker.Unsubscribe("client-123")

// Publish events from anywhere
broker.Publish(stream.Event{
    Type: "node_end",
    Data: map[string]any{"node": "extract", "status": "done"},
})
```

### HTTP Handler

The broker includes an SSE HTTP handler:

```go
http.Handle("/events", broker.SSEHandler("client-123"))
```

Clients connect via standard `EventSource`:

```javascript
const source = new EventSource("/events");
source.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log(data.type, data);
};
```

## Combining Model and Graph Streaming

For agents that use both a model and a graph, you can wire model streaming into graph node functions:

```go
chatNode := func(ctx context.Context, s graph.State) (graph.State, error) {
    ch, err := provider.StreamChat(ctx, &model.ChatRequest{
        Messages: []model.Message{
            {Role: "user", Content: s["query"].(string)},
        },
    })
    if err != nil {
        return s, err
    }

    var response strings.Builder
    for chunk := range ch {
        response.WriteString(chunk.Content)
        // Optionally publish to broker for UI updates
    }
    s["response"] = response.String()
    return s, nil
}
```
