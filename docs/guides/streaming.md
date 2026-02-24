---
title: "Streaming & SSE"
permalink: /guides/streaming/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
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
