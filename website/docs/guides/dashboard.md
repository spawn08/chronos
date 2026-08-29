---
title: "Dashboard (Visual Studio / Graph Debugger)"
sidebar_label: "Dashboard"
---

The dashboard is a live visual debugger for StateGraph runs, served by ChronosOS at
`/dashboard/`. It lets you watch a run's graph execute, inspect per-node state, rewind
to any checkpoint (time-travel), resume a paused run, and see per-session cost — all
from a browser, with no other tooling.

```
os/server.go ──► /dashboard/        static UI (HTML/JS/CSS, no build step, no CDN)
             ──► /api/dashboard/*   checkpoints · graph topology · cost · resume · time-travel
```

Session listing, traces, live streaming, and HITL approvals are **not** duplicated
under `/api/dashboard/` — the UI calls the existing `/api/sessions`, `/api/traces`,
`/api/agui/stream`, and `/api/approval/*` endpoints directly.

## Enabling it

The dashboard is on by default on any `chronosos.Server`:

```go
s := chronosos.New(":8420", store)
// http://localhost:8420/dashboard/
```

Disable it on a hardened deployment with `WithDashboard(false)`, the same pattern as
`WithSwagger(false)`.

## Wiring graph rendering and resume/time-travel

The dashboard's graph view and its resume/time-travel actions need to know which
compiled graph a session belongs to. Register your graphs, keyed by the **agent id**
that produced the session (`storage.Session.AgentID`):

```go
s := chronosos.NewWithOptions(":8420", store,
    chronosos.WithGraphs(dashboard.GraphRegistry{
        "expense-approver": compiledGraph,
    }),
)
```

Without `WithGraphs`, the dashboard still lists sessions, checkpoints, and cost; the
graph view and the Resume/time-travel buttons return `501 Not Implemented`.

## Cost reporting

Wire an already-configured `engine/hooks.CostTracker` (the same one you attach to your
agent's hook chain) to enable the per-session token/cost panel:

```go
costTracker := hooks.NewCostTracker(priceTable)
// ... agent.WithHooks(hooks.Chain{costTracker, ...}) ...

s := chronosos.NewWithOptions(":8420", store,
    chronosos.WithGraphs(graphs),
    chronosos.WithCostTracker(costTracker),
)
```

Without `WithCostTracker`, the cost panel shows "no cost tracker configured" rather
than a fabricated $0.

## Resuming a paused run

`graph.Runner` mirrors a run's status (`running`/`paused`/`completed`/`failed`) onto
`storage.Session.Status`, so the session list shows which sessions are paused without
loading a checkpoint. Clicking **Resume** on a paused session calls
`POST /api/dashboard/resume {"session_id": ...}`, which builds a fresh `graph.Runner`
against the registered graph and calls `Resume` — the same path
`sdk/agent.Agent.Resume` and `graph.Runner.Resume` use.

## Time-travel

The checkpoint list (`GET /api/dashboard/checkpoints?session_id=`) shows every
checkpoint in a session's history. Clicking one calls
`POST /api/dashboard/timetravel {"checkpoint_id": ...}`, which rewinds execution to
that checkpoint's node and state and re-runs from there — `graph.Runner.ResumeFromCheckpoint`
under the hood. The original history is untouched; re-running from an earlier
checkpoint simply continues forward from that point.

## Live updates

The graph view highlights the currently executing node by subscribing to the
standardized AG-UI stream (`/api/agui/stream?session=<id>`, [see the AG-UI guide](agui.md))
and listening for `STEP_STARTED` events.

## Auth

Every `/api/dashboard/*` call goes through the same auth/tenant chain as every other
`/api/` route (a session or checkpoint from another tenant is invisible, exactly like
`/api/sessions` and `/api/traces`). The static UI shell itself is served **without**
auth — like the Swagger UI — so a bearer token or API key can be entered from the page
before making any API call:

```
GET /dashboard/         → always reachable (static HTML/JS/CSS only)
GET /api/dashboard/*    → requires auth when auth is enabled
```

Paste a token into the input in the dashboard's header; it's stored in
`localStorage` and attached as `Authorization: Bearer <token>` (JWTs) or `X-Api-Key`
(anything else) to every fetch call.

**Caveat:** browsers' `EventSource` cannot set custom headers, so the live
`/api/agui/stream` connection only works unauthenticated, or behind a reverse proxy
that injects the header for that path. The rest of the dashboard (sessions,
checkpoints, graph, cost, resume, time-travel) works fully authenticated either way —
only the live node-highlighting stream is affected.

## Full example

See [`examples/dashboard/`](https://github.com/spawn08/chronos/tree/main/examples/dashboard)
for a runnable, key-free demo: it runs a small expense-approval workflow to its
human-in-the-loop gate, then serves the dashboard so you can inspect the paused
session, time-travel through its checkpoints, and resume it to completion.

```bash
go run ./examples/dashboard/
```
