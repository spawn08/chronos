# Workstream C — Developer Experience & Eval-Driven Loop

> **Wave 2.** LangSmith + LangGraph Studio are the reason LangGraph "quietly became the default"
> — the tight trace→dataset→eval→gate loop and a visual debugger. The eval loop (WC-C-001), the
> visual debugger (WC-C-002), and YAML agents' integration with it (WC-C-004) are all shipped;
> only one-command deploy (WC-C-003) remains.

---

### WC-C-001 — Eval-driven dev loop (trace → dataset → eval → gate)
- [x] **Status:** DONE <!-- done: 2026-07-31 -->
- **Delivered:** `evals/dataset.go` (`CaptureFromSession` builds datasets from the tenant-scoped
  event ledger), `evals/runner.go` (`Target` seam + `DatasetRunner` → `DatasetReport`),
  `evals/gate.go` (`Gate` with min-score/min-pass-rate/max-regression), `evals/store.go`
  (tenant-scoped, checkpoint-backed `StorageReportStore` for trend history). CLI: `chronos evals
  capture|gate|history`; CI `eval-gate` job rejects regressions. Example `examples/eval_loop/`,
  docs `website/docs/guides/eval-loop.md`.

---

### WC-C-002 — Visual studio / graph debugger
- [x] **Status:** DONE <!-- done: 2026-08-29 -->
- **Delivered:** `os/dashboard` — a `Handler` serving checkpoint history, graph topology
  (`engine/graph.ToJSON`, new alongside `ToMermaid`/`ToDOT`), per-session cost
  (`engine/hooks.CostTracker.GetSessionCost`), and resume/time-travel actions that build a fresh
  `graph.Runner` against an app-supplied `GraphRegistry` (compiled graphs keyed by agent id).
  Session listing, traces, live streaming, and HITL approvals are **not** duplicated — the UI
  calls the existing `/api/sessions`, `/api/traces`, `/api/agui/stream`, and `/api/approval/*`
  endpoints directly. Mounted at `/dashboard/` (static UI, embedded via `go:embed`, no CDN, no
  build step) and `/api/dashboard/*` (tenant-scoped like every other `/api/` route) via new
  `chronosos.WithGraphs`/`WithCostTracker`/`WithDashboard(false)` options; the static shell
  bypasses auth like Swagger so a token can be entered from the page itself. Live node
  highlighting subscribes to the AG-UI stream's `STEP_STARTED` events. Fixed two real bugs
  surfaced while building this: `graph.Runner` never mirrored a run's paused/completed status onto
  `storage.Session.Status` (so no caller — including this dashboard and the CLI's session
  list/monitor — could tell a session was paused without loading a checkpoint), and the in-memory
  storage adapter's checkpoints aliased a single mutable state map across an entire run (breaking
  time-travel's point-in-time snapshot guarantee) and ordered `GetLatestCheckpoint` by wall-clock
  instead of `seq_num` (the same non-determinism P0-003 had already fixed for the SQL adapters,
  just missed here). Example `examples/dashboard/` (key-free, runs to a HITL gate), docs
  `website/docs/guides/dashboard.md`.
- **Review gates:** design-pattern-reviewer + code-quality-auditor both ran and returned
  REQUEST_CHANGES; both independently converged on the same CRITICAL finding, plus three BLOCKs,
  all fixed in-branch: (CRITICAL) `handleCost` forwarded `session_id` straight to
  `hooks.CostTracker` — a plain, tenant-unaware map — letting any authenticated caller read
  another tenant's cost by guessing its session id; now gated behind a tenant-scoped
  `GetSession` first. (BLOCK) the new per-run `Session.Status` sync was a third whole-record
  `GetSession`→mutate→`UpdateSession` writer racing with the planning/VFS tools' own Metadata
  writer; added an optional `storage.SessionStatusUpdater` (narrow status-only write, mirroring
  the `Retention`/`Paginator`/`SessionFileStore` optional-capability pattern) implemented in the
  memory/sqlite/postgres adapters, with a regression test proving a concurrent Metadata write
  survives. (BLOCK) `resolveGraph` conflated "session not found" (should be 404) with "no graph
  registered" (501) into one error; split into `requireSession`/`graphForSession`. (BLOCK)
  `handleResume`/`handleTimeTravel` bypassed `os/server.go`'s 413-vs-400 body-size distinction;
  added a matching `decodeJSON` helper. (BLOCK) `os/dashboard/ui/app.js` interpolated
  server-sourced ids/names into `innerHTML` unescaped in four places; switched to
  `createElement`/`textContent` throughout, matching the file's otherwise-consistent safe usage.
- **Problem:** `os/dashboard/` is an empty scaffold and `engine/graph/visualize.go` only renders
  static graphs. There is no way to step through a run, inspect state per node, or use the
  existing **time-travel** (PLAN.md P1) visually — a major gap vs LangGraph Studio.
- **Acceptance:** A developer opens a run in the dashboard, watches nodes execute live, inspects
  state at any node, rewinds to a checkpoint, and approves a paused HITL step — all in the UI.
- **Depends on:** WC-B-003 (standardized event stream), WC-C-001 optional (eval results panel).
- **Tests:** `os/dashboard/dashboard_test.go` (table-driven handler tests, tenant isolation, a
  full pause→resume and run→time-travel functional test against a real `graph.Runner`);
  `os/server_dashboard_test.go` (auth bypass for the UI shell, traversal safety, tenant isolation
  at the HTTP layer, `WithDashboard(false)`); `engine/graph/visualize_test.go` (`ToJSON` + a
  benchmark); regression tests for both fixed bugs (`engine/graph/runner_durability_test.go`,
  `storage/adapters/memory/memory_test.go`). Full repo `-race` suite green.

---

### WC-C-004 — YAML agents as durable, dashboard-visible ChronosOS runs
- [x] **Status:** DONE <!-- done: 2026-08-29 -->
- **Delivered:** a declarative graph schema on `AgentConfig`
  (`sdk/agent/config.go` `Graph *GraphConfig`, `Durable bool`) and its compiler
  (`sdk/agent/graphbuild.go`) — four node types (`model`, `tool`, `subagent`,
  `passthrough`, the last doubling as the building block for an `interrupt:
  true` HITL gate) and static/conditional edges, compiling to a real
  `graph.StateGraph`/`CompiledGraph` via the existing `Agent.WithGraph` builder
  method, so `Agent.Run`/`Resume` behave exactly as they do for a hand-built Go
  graph (session creation, checkpointing, pause/resume) — zero changes to
  `engine/graph` or `Agent.Run` itself. `chronos serve` (`cli/cmd/serve.go`
  `buildServeGraphOptions`) now optionally loads the same agents YAML
  `run`/`repl` already resolve (`-c`/`CHRONOS_CONFIG`) and registers every
  `durable: true` agent's compiled graph into `chronosos.WithGraphs`
  automatically — the exact wiring `examples/dashboard/main.go` previously did
  by hand. A new `POST /api/dashboard/runs` (`os/dashboard/dashboard.go`)
  closes a second, deeper gap found while building this: **no HTTP endpoint
  anywhere in ChronosOS could start a new run at all** — `handleResume`/
  `handleTimeTravel` only ever acted on a session some in-process caller had
  already created by calling `graph.Runner.Run` before the server started;
  this affected Go-defined graphs too, not just YAML. Also added `chronos auth
  token` (`cli/cmd/auth.go`, using the existing `auth.CreateTestToken` HS256
  signer) to close the "no way to get a local credential" onboarding gap, and
  fixed two confirmed doc inaccuracies in `website/docs/guides/server.md` (the
  false `.chronos/agents.yaml` auto-read claim; the options table implying
  `WithScheduler`/`WithApproval` gate their routes when both have in-process
  defaults). Example `examples/yaml_dashboard/` (runs via `chronos serve`
  alone, zero Go code), docs `website/docs/guides/yaml-dashboard.md`.
- **Problem:** a plain YAML `AgentConfig` had no way to express a graph, so
  `Agent.Run` (which only creates a session/checkpoints when `Graph != nil`)
  silently skipped storage entirely for every YAML agent — nothing for
  `/api/sessions`, `/api/traces`, or the dashboard (WC-C-002) to show. Separately,
  `chronos serve` never loaded agent YAML at all, and `chronos deploy` loaded it
  but never touched `chronosos` or started a server — there was no path from
  "I have an agent.yaml" to "it's a durable, dashboard-visible ChronosOS run."
- **Acceptance:** a YAML-only `agent.yaml` with a `graph:` block and
  `durable: true`, run via `chronos -c agent.yaml serve`, shows up in
  `/api/sessions`, pauses at an interrupt node, and can be started/resumed/
  time-traveled entirely through the dashboard/API — verified manually against
  a running server (start → pause at gate → resume → completed) in addition to
  the automated tests below.
- **Review gates:** design-pattern-reviewer + code-quality-auditor both ran and
  returned REQUEST_CHANGES; both independently converged on real, distinct
  issues, all fixed in-branch. Design: (CRITICAL) `handleStartRun` accepted a
  caller-supplied `session_id` for a brand-new session with no tenant-scoped
  check — since session id is a globally-unique key across every storage
  adapter, this was a cross-tenant existence oracle (200 vs. error) and let one
  tenant squat an id; fixed by always server-generating the session id, like
  every other new-session path in the codebase (`Agent.Run`, `graph.Runner`).
  (CRITICAL) `buildServeGraphOptions` opened/migrated each YAML agent's own
  `storage:` backend via `agent.BuildAll` and then discarded it — the
  dashboard's `Runner` always persists through the server's separately
  resolved main store, so a durable agent's `storage:` block was silently
  irrelevant to where its sessions actually land, contradicting the guide and
  example's own wiring; fixed by documenting the single-shared-store reality
  in `buildServeGraphOptions`'s doc comment and closing every per-agent store
  immediately after its graph is extracted (also closes the BLOCK leak below).
  (BLOCK) every `storage.Storage` `agent.BuildAll` opened in that function
  leaked (never `Close`d) for every agent in the file, durable or not; fixed by
  the same change. Code quality: (CRITICAL) the `tool` node type called a
  `tool.Definition`'s `Handler` directly instead of `tool.Registry.Execute`,
  bypassing `deny`/`require_approval`/confirmation/user-input enforcement — a
  graph-declared tool was a backdoor around the same checks a model-initiated
  call would face; fixed by routing through `Execute`. (CRITICAL) the `model`
  node type called `a.Model.Chat` directly instead of `Agent.Chat`, dropping
  system prompt, guardrails, memory/RAG, hooks, tracing, and tool-calling;
  fixed by routing through `Agent.Chat` (which also means a `model` node can
  now call tools). (BLOCK) `buildServeGraphOptions` swallowed every
  `loadAgentConfig` error identically, including a malformed/invalid config —
  not just "no config found"; fixed with a new `agent.ErrConfigNotFound`
  sentinel so only that specific case is treated as a no-op. (BLOCK)
  `WithPeerAgents` shipped as dead exported API with no caller or test; fixed
  by adding `TestBuildAgentWithPeerAgents`. (BLOCK) `examples/yaml_dashboard`
  duplicated the registry-building loop with no nil-safety (would panic with
  zero durable agents); fixed by extracting the shared `agent.DurableGraphs`
  helper both call. (BLOCK) `tool`/`subagent` nodes silently fell back to the
  whole state or an empty message when `input_key` resolved to the wrong type;
  fixed by returning a clear error instead. One NOTE (no cross-agent
  `subagent` cycle detection, so a misconfigured A→B→A reference chain can
  recurse unboundedly at runtime) was not fixed — logged as a known follow-up
  below, matching PLAN.md's P2-002 precedent for documenting an accepted gap
  rather than silently dropping it.
- **Known follow-up (not fixed):** `validateFileConfig` checks that a
  `subagent` graph node references an existing agent id, but does not detect
  reference cycles across separate agents' graphs (A's graph calls B via a
  `subagent` node, B's graph calls A back). `sub.Chat` has no call-depth guard,
  so a cyclic YAML config can recurse until the stack is exhausted. Add either
  a build-time cycle check across `subagent` edges or a runtime call-depth
  limit before treating any of this as safe against untrusted/generated YAML.
- **Depends on:** WC-C-002 (dashboard/`GraphRegistry`/`ToJSON`).
- **Tests:** `sdk/agent/graphbuild_test.go` (schema validation table, all four
  node types executing end-to-end through `BuildAgent`+`Agent.Run`/`Resume`
  including the interrupt gate, cross-agent `subagent` peer resolution via
  `BuildAll` and via direct `WithPeerAgents`, tool-node permission enforcement,
  input_key type-mismatch errors, `durable`-requires-graph/persistent-storage
  validation); `os/dashboard/dashboard_test.go` (`TestHandler_StartRun`:
  400/501/405, always-server-generated session id even when a client supplies
  one, input seeding; `TestHandler_StartRun_TenantScoping`); `cli/cmd/
  serve_graph_test.go` (`buildServeGraphOptions`: no-config no-op, malformed
  config surfaces an error, storage is closed not leaked (`TestCloseAgentStorage`);
  non-durable agent not registered, durable agent registered, build-error
  propagation); `cli/cmd/auth_test.go` (`chronos auth token` across
  none/apikey/jwt modes). Full repo `-race` suite green.

---

### WC-C-003 — One-command deploy
- [ ] **Status:** TODO
- **Problem:** `cli/cmd/deploy.go` and the hardened Helm chart (`deploy/helm/chronos/`, PLAN.md
  P2-004) exist, but there is no single frictionless path from `agent.Build()` to a running,
  observable service. LangGraph Platform / DeepAgents Deploy win on this.
- **Location:** `cli/cmd/deploy.go`, `deploy/helm/chronos/`, `deploy/docker/`,
  `sdk/agent/config.go` (YAML agent config), `examples/team_deploy/`.
- **Action:** `chronos deploy` takes an agent/team (code or YAML config) and produces a running
  deployment (local Docker, or k8s via the Helm chart) with the control plane, SSE, metrics
  endpoint, and dashboard wired — TLS/probes/HPA on by defaults from P2-004. Print the dashboard
  and API URLs on success.
- **Acceptance:** From a fresh checkout, one command yields a reachable, authenticated,
  observable agent service with the dashboard live; documented in a quickstart.
- **Depends on:** WC-C-002 (dashboard is part of "observable").
- **Tests:** deploy-command unit tests (extend `cli/cmd/` tests); a scripted smoke deploy to a
  local k8s (kind) gated behind `testing.Short()`/env.
