# Workstream C — Developer Experience & Eval-Driven Loop

> **Wave 2.** LangSmith + LangGraph Studio are the reason LangGraph "quietly became the default"
> — the tight trace→dataset→eval→gate loop and a visual debugger. Chronos has the raw materials
> (`evals/` package with accuracy/performance/reliability, `os/trace/otel.go` OTLP export,
> `engine/graph/visualize.go`, `os/dashboard/` scaffold, `cli/cmd/deploy.go`) but not the *loop*
> or the *visual layer*. This workstream turns pieces into a product.

---

### WC-C-001 — Eval-driven dev loop (trace → dataset → eval → gate)
- [ ] **Status:** TODO
- **Problem:** `evals/` can score runs, but there is no loop: capture real traces, curate them
  into datasets, run evals against them, and gate regressions in CI. This is the core LangSmith
  workflow and the biggest DX moat.
- **Location:** `evals/` (extend `eval.go`, `loader.go`, `accuracy.go`); trace source from
  `os/trace/` collector; a new `evals/dataset.go`; CLI command in `cli/cmd/` (add an `evals`
  subcommand next to `serve.go`/`deploy.go`); CI wiring in `.github/workflows/ci.yml`.
- **Action:** (1) Capture: turn stored traces (`storage.Storage` traces/events) into eval
  datasets. (2) Run: execute a dataset against an agent/graph and score with existing evaluators
  plus an LLM-as-judge evaluator. (3) Gate: `chronos evals run --gate` fails CI when scores
  regress past a threshold. Store dataset + results (tenant-scoped) for trend comparison.
- **Acceptance:** A developer captures traces from a run, builds a dataset, runs evals, and a
  regression fails CI; results are queryable over time.
- **Depends on:** none (builds on shipped `evals/` + trace).
- **Tests:** dataset build/round-trip tests; gate pass/fail tests with synthetic score deltas;
  a CI job demonstrating the gate.

---

### WC-C-002 — Visual studio / graph debugger
- [ ] **Status:** TODO
- **Problem:** `os/dashboard/` is an empty scaffold and `engine/graph/visualize.go` only renders
  static graphs. There is no way to step through a run, inspect state per node, or use the
  existing **time-travel** (PLAN.md P1) visually — a major gap vs LangGraph Studio.
- **Location:** `os/dashboard/` (build the UI + backing API), reuse `engine/graph/visualize.go`,
  the per-session SSE stream (WC-B-003 / P1-015), checkpoints in `storage.Storage`, and the
  time-travel resume path in `engine/graph/runner.go`.
- **Action:** A web UI (served by `os/server.go`) that: lists sessions/runs, renders the graph
  live, shows per-node state + token/cost/latency (from P1-017 metrics), lets a developer
  time-travel to a checkpoint and re-run, and drives HITL approvals. Read-only data via a
  dashboard API; mutations go through existing authorized endpoints.
- **Acceptance:** A developer opens a run in the dashboard, watches nodes execute live, inspects
  state at any node, rewinds to a checkpoint, and approves a paused HITL step — all in the UI.
- **Depends on:** WC-B-003 (standardized event stream), WC-C-001 optional (eval results panel).
- **Tests:** dashboard API handler tests (extend `os/server_*_test.go`); an e2e smoke test of
  the live-run view against a mock provider.

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
