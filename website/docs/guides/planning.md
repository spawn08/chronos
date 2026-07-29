---
title: "Planning (Todo) Tool"
sidebar_label: "Planning"
---


Long, multi-step tasks drift: the model loses track of subgoals, repeats work, or
forgets a step. The built-in **planning tool** gives an agent a first-class,
revisable task list — a "todo plan" — that it maintains across turns. The plan is
persisted per session, so it survives checkpoints and resumes with a durable run.

This is the first piece of the Chronos *agent harness* (planning → context
offloading → subagents → compaction).

## Quick start

```go
import (
    "github.com/spawn08/chronos/engine/tool/builtins"
    "github.com/spawn08/chronos/sdk/agent"
)

// A durable store: the plan is kept in the session record and survives a resume.
planStore := builtins.NewStoragePlanStore(store) // store is your storage.Storage

a, _ := agent.New("research-agent", "Research Assistant").
    WithModel(provider).
    WithStorage(store).
    AddToolkit(builtins.NewPlanToolkit(planStore, broker)). // broker may be nil
    Build()

resp, _ := a.ChatWithSession(ctx, "session-1", "Research topic X and write a report.")
```

`NewPlanToolkit` registers a single tool, `update_plan`, that the model calls to
create and revise its task list. Pass a `stream.Broker` to have plan updates
streamed (see [Streaming](/guides/streaming)), or `nil` to skip streaming.

## How the model uses it

The model sends the **complete** task list on every call; it replaces the stored
plan. Each task has `content` and a `status` of `pending`, `in_progress`, or
`completed`. A typical progression:

| Turn | Plan the model writes |
|------|-----------------------|
| 0 | `[~] gather sources`, `[ ] draft`, `[ ] review` |
| 1 | `[x] gather sources`, `[~] draft`, `[ ] review` |
| 2 | `[x] gather sources`, `[x] draft`, `[~] review` |
| 3 | `[x] gather sources`, `[x] draft`, `[x] review` |

The tool result echoes the current plan (a rendered checklist plus the structured
task array and a `complete` flag) so the model always sees its own progress.

## Persistence and resume

Two stores implement the `builtins.PlanStore` interface:

| Store | Durability | Use for |
|-------|-----------|---------|
| `NewInMemoryPlanStore()` | process-local, lost on restart | dev, tests, ephemeral chats |
| `NewStoragePlanStore(store)` | persisted in the session record | production, durable runs |

`StoragePlanStore` keeps the plan as a single mutable value in the session's
`Session.Metadata` — it does not touch the runner's append-only event ledger or
its sequence space. A worker resuming a session — or a process restarting after a
crash — reloads the plan exactly as it was:

```go
resumeCtx := storage.WithSession(ctx, "session-1")
plan, _ := builtins.NewStoragePlanStore(store).Load(resumeCtx)
fmt.Println(plan.Summary()) // the plan, intact after resume
```

Both stores scope the plan to the **session and tenant** carried by the context.
The graph runner and `ChatWithSession` set the session automatically; two tenants
(or two sessions) never see each other's plans.

:::note
Planning **requires an active session**. Use `ChatWithSession` or a graph run —
both put the session id in the context. Calling the plain `Chat` method (which
has no session) while the planning toolkit is registered makes `update_plan`
fail with `ErrNoSession`; this is deliberate, so a plan is never silently written
to a shared, sessionless scope.
:::

## Streaming plan updates

When the agent has a `stream.Broker`, every plan change publishes a
`stream.EventPlanUpdate` event, routed to the session's topic. UIs subscribe to
render a live progress checklist without polling.

## Example

A complete, runnable example (no API keys required) is in
[`examples/planning_agent`](https://github.com/spawn08/chronos/tree/main/examples/planning_agent).
It drives an agent through a three-step task and reloads the plan from a fresh
store to prove it persisted across a resume.
