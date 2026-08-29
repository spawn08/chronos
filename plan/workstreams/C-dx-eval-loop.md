# Workstream C — Developer Experience & Eval-Driven Loop

> **Wave 2.** LangSmith + LangGraph Studio are the reason LangGraph "quietly became the default"
> — the tight trace→dataset→eval→gate loop and a visual debugger. The eval loop (WC-C-001) and
> the visual debugger (WC-C-002) are both shipped; only one-command deploy (WC-C-003) remains.

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
