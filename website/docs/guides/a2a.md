---
title: "Agent-to-Agent (A2A)"
sidebar_label: "A2A Protocol"
---

The **Agent-to-Agent (A2A)** protocol lets agents from different frameworks call
one another over HTTP. Chronos is both an A2A **server** (expose a Chronos agent
so external clients — ADK, LangGraph, DeepAgents, or another Chronos — can
discover and invoke it) and an A2A **client** (delegate a task to a remote agent
and stream the result back, the same way you'd spawn a local subagent).

## Protocol surface

All routes are mounted under `/a2a`:

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/a2a/agent` | Fetch the agent card (capabilities/skills) |
| `POST` | `/a2a/tasks` | Create (delegate) a task |
| `GET` | `/a2a/tasks/{id}` | Poll task status/result |
| `GET` | `/a2a/tasks/{id}/stream` | Stream task updates as Server-Sent Events |
| `DELETE` | `/a2a/tasks/{id}` | Cancel a task |

A task moves through `pending → running → completed | failed | cancelled`.

## Server: expose a Chronos agent

An `a2a.Server` pairs an **agent card** with a **handler** that does the work. Build
the card straight from a skill registry so peers discover exactly what the agent
can do:

```go
skills := skill.NewRegistry()
skills.Register(&skill.Skill{Name: "summarize", Version: "1.0"})

card := a2a.CardFromSkills("chronos-agent", "A Chronos A2A agent", "1.0", skills)

srv := a2a.NewServer(card, func(ctx context.Context, task *a2a.Task) error {
    task.Output = doWork(task.Input) // your agent run
    return nil
})
http.Handle("/a2a/", srv) // or mount on the control plane — see below
```

`NewServer` uses an in-memory store: simple, but tasks are lost on restart.

### Durable, resumable tasks

For long-running work, back tasks with the durable queue (`engine/queue`) so they
survive restarts and are re-leased if a worker dies (orphan recovery). The task
record is persisted as a per-tenant checkpoint via `storage.Storage`.

```go
ds := a2a.NewDurableStore(queue, store, handler)

// A worker drives execution; a reaper recovers orphaned tasks after a crash.
w, _ := queue.NewWorker(queue, ds.Executor, queue.WorkerConfig{ID: "a2a-worker-1"})
go w.Run(ctx)
go queue.NewReaper(queue, time.Second).Run(ctx)

srv := a2a.NewServerWithStore(card, ds)
```

The two backends implement the same `a2a.TaskStore` interface, so the HTTP surface
is identical either way.

## Serving on the control plane (auth + tenancy)

Mount the server on ChronosOS with `WithA2A` to put it behind the auth middleware
chain and scope every task to the caller's tenant:

```go
srv := chronosos.NewWithOptions(":8420", store,
    chronosos.WithAPIKeyAuth(apiKeyCfg),
    chronosos.WithA2A(a2aServer), // a2aServer is an *a2a.Server (an http.Handler)
)
```

- Requests to `/a2a/*` require authentication (the route is **not** exempt).
- The tenant is derived from the authenticated principal, never from client input.
  A task created by one tenant is invisible to another — a cross-tenant
  `GET /a2a/tasks/{id}` resolves to **404**, closing the IDOR.

## Client: delegate to a remote agent

```go
client := a2a.NewClient("https://peer.example.com")

card, _ := client.GetAgentCard(ctx)          // discover
task, _ := client.CreateTask(ctx, "do X", nil) // delegate

// Stream updates until the task reaches a terminal state.
tasks, errs := client.StreamTask(ctx, task.ID)
for snap := range tasks {
    fmt.Println(snap.Status, snap.Output)
}
if err := <-errs; err != nil { /* ... */ }
```

`WaitForCompletion` is a polling alternative when you don't need incremental updates.

### Remote agent as a tool (delegated subagent)

`NewRemoteAgentTool` adapts a remote A2A agent into a `tool.Definition`, so a
Chronos model can delegate a sub-task to it — composing with the subagent model.
The tool submits the task, prefers the streamed result (falling back to polling for
peers without a stream endpoint), and returns only the remote agent's final output,
so its intermediate work never enters the caller's context.

```go
delegate := a2a.NewRemoteAgentTool(
    "research_agent",
    "Delegate a research task to the remote research agent.",
    client,
    a2a.WithPermission(tool.PermRequireApproval), // gate outbound delegation
)
agent.New("assistant", "Assistant").AddTool(delegate).Build()
```

## Example

A complete, key-free round-trip (durable server + discover + delegate + stream +
tool delegation) lives in
[`examples/a2a_interop/main.go`](https://github.com/spawn08/chronos/blob/main/examples/a2a_interop/main.go):

```bash
go run ./examples/a2a_interop/
```
