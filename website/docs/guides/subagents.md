---
title: "Context-Isolated & Dynamic Subagents"
sidebar_label: "Subagents"
---


Delegation is how an agent stays focused on a long task: it hands a self-contained
sub-task to a **subagent** that works in its own fresh conversation and returns
only the result. The subagent's intermediate reasoning and tool calls never enter
the parent's context window — the parent pays only for the final answer. This is
the third piece of the Chronos agent harness, after
[planning](/guides/planning) and the [virtual filesystem](/guides/virtual-filesystem).

## Quick start

```go
import (
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/harness"
)

parent, _ := agent.New("lead", "Lead").WithModel(provider).Build()

// Derive a subagent service from the built parent (subagents inherit its model
// and draw tools from its registry), register specialists, attach the tool.
svc, _ := harness.NewSubAgentService(parent)
svc.Register(harness.SubAgentSpec{
    Name:         "researcher",
    Description:  "Researches a topic and returns a concise finding.", // shown to the model
    SystemPrompt: "You are a focused researcher.",
    ToolNames:    []string{"web_search"}, // a subset of the parent's tools
})
harness.Attach(svc, harness.NewInProcessRunner(svc))
```

`Attach` registers a single `spawn_subagent` tool on the parent (the service
already holds the parent's registry). `NewSubAgentService` is called **after**
`Build()` because the service is derived from the built agent; a builder method
is intentionally not offered (the `agent` package must not depend on `harness`).
Each registered subagent's `Description` is surfaced in the tool description so
the model knows what each specialist does. The service is a concurrency-safe
registry: `Register` may be called while spawns are in flight.

## Context isolation

When the model calls `spawn_subagent`, Chronos builds a fresh agent with only the
subagent's own system prompt and the delegated task — none of the parent's
conversation. It runs its own tool-calling loop, and the tool returns just:

```json
{ "agent": "researcher", "result": "…the final finding…" }
```

So the parent's context grows by one short result, not by the subagent's entire
transcript. This is what lets a coordinator drive many sub-tasks without
overflowing its window.

## Dynamic subagents

A subagent doesn't have to be registered ahead of time. The model can invent one
per task by passing a `system_prompt` (and an optional `tools` subset) directly
in the `spawn_subagent` call:

```jsonc
{
  "task": "Summarize these findings into three bullets.",
  "system_prompt": "You are a precise summarizer.",
  "tools": ["fs_read"]
}
```

Granted tool names must exist in the parent's registry; unknown names are
rejected. A `spawn_subagent` call that names a **registered** `agent` that
doesn't exist fails closed (so a typo surfaces as an error rather than silently
running an ad-hoc agent). Nesting is bounded by `WithMaxDepth` (default 3) so a
subagent can't recurse without limit — the depth is carried across the durable
queue too, so the bound holds even when delegation runs on another worker.

## Durable delegation

`InProcessRunner` runs the subagent in the calling process. For long or critical
sub-tasks, use `QueuedRunner` instead: it enqueues the subagent as a durable
graph run on the [work queue](/guides/durable-execution), so if the worker
executing it dies, the reaper re-leases the run and another worker completes it —
the subagent is resumable and can run on any node.

```go
import (
    "time"

    "github.com/spawn08/chronos/engine/queue"
    "github.com/spawn08/chronos/sdk/harness"
)

// q is a *queue.Queue and store a storage.Storage, both already constructed.
runner := harness.NewQueuedRunner(svc, q, store, harness.WithTimeout(30*time.Second))
harness.Attach(svc, runner)
```

`WithTimeout` bounds how long a spawn waits for a worker; without it the call
waits until its context is done, so pass a timeout (or a cancelable context) if
no worker may be draining the queue.

Durable delegation requires the subagent to be **registered** (a remote worker
rebuilds it by name), and every worker must run the shared subagent graph:

```go
import (
    "context"
    "time"

    "github.com/spawn08/chronos/engine/graph"
    "github.com/spawn08/chronos/engine/queue"
    "github.com/spawn08/chronos/sdk/harness"
)

// svc, store, and q (a *queue.Queue) are the same instances from above; ctx is
// a long-lived context this worker runs under.
g, _ := harness.NewSubAgentGraph(svc)
exec := graph.NewQueuedExecutor(store, graph.SingleGraphResolver(g)).Executor()
worker, _ := queue.NewWorker(q, exec, queue.WorkerConfig{ID: "w1", Lease: 30 * time.Second})
go worker.Run(ctx)
```

Dynamic (inline) subagents can only use `InProcessRunner`, since another worker
cannot reconstruct a subagent that was described at runtime.

## Example

A complete, runnable example (no API keys) is in
[`examples/subagents`](https://github.com/spawn08/chronos/tree/main/examples/subagents).
It shows a lead agent delegating to a researcher and receiving only the finding.
