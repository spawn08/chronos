# Workstream A — Agent Harness

> **Wave 1.** Highest-leverage gap. Chronos's agent loop (`sdk/agent/agent.go` `Chat` /
> `streamLoop`) is a competent tool-calling loop, but it lacks the *harness* — the built-in
> scaffolding that makes agents work on long, multi-step tasks. DeepAgents proved this
> (planning + virtual filesystem + subagents + compaction) is what separates a demo from a
> product. Several seams already exist and should be **upgraded, not rebuilt**:
> `AddSubAgent(sub *Agent)` (agent.go:170), `WithContextConfig(ContextConfig)` (agent.go:115,
> `sdk/agent/context.go`), and `WithMemoryManager` (agent.go:110).

New package suggested: `sdk/harness/` for orchestration primitives that compose over the
existing agent loop, plus built-in tools under `engine/tool/builtins/`.

---

### WC-A-001 — Built-in planning ("todo") tool
- [x] **Status:** DONE <!-- done: 2026-07-29 -->
  - **Delivered:** `engine/tool/builtins/plan.go` — `Plan`/`PlanTask`/`TaskStatus` domain,
    a session+tenant-scoped `PlanStore` interface with `InMemoryPlanStore` (ephemeral) and
    `StoragePlanStore` implementations, and the `update_plan` tool + `NewPlanToolkit`.
    `StoragePlanStore` persists the plan as a single mutable value in `Session.Metadata` — it
    does **not** touch the runner's append-only event ledger or its sequence space, so it adds no
    seq-collision or pagination-corruption risk. Both stores reject a sessionless context
    (`ErrNoSession`) so they are behaviorally substitutable (LSP). Plan updates publish
    `stream.EventPlanUpdate` to the session topic. Session scoping added via
    `storage.WithSession`/`SessionFromContext`; the graph runner and `ChatWithSession` now carry
    the session id in context so tools resolve it without signature changes. Wired through the
    existing `AddToolkit(builtins.NewPlanToolkit(store, broker))` (no bespoke builder method).
    Runnable key-free example `examples/planning_agent/main.go` proves the plan survives a real
    store close/reopen (restart); docs at `website/docs/guides/planning.md`. Tests: `plan_test.go`
    (table-driven + `-race` concurrent saves + LSP substitutability) and `plan_durability_test.go`
    (persists across a fresh instance and a runner pause→resume, asserts the event ledger keeps
    unique seq numbers, `-race` same-session concurrent saves, and a `Benchmark`).
  - **Review gates:** design-pattern-reviewer + code-quality-auditor both ran; their
    REQUEST_CHANGES findings (ledger seq collision/TOCTOU, LSP divergence, O(n) ledger scans,
    SDK→builtins coupling, example overselling durability, dead `id` field) were all fixed
    in-branch by moving persistence to `Session.Metadata`.
- **Problem:** The agent cannot maintain and revise an explicit plan across turns. Long tasks
  drift and lose track of subgoals. ADK, LangGraph, and DeepAgents all give the model a
  first-class planning primitive.
- **Location:** new `engine/tool/builtins/plan.go` (alongside `calculator.go`, `file.go`, `shell.go`);
  register via `engine/tool/registry.go`; expose through `sdk/agent` builder (`AddToolkit`).
- **Action:** Add a `plan`/`todo` tool the model calls to create, update, and complete a
  structured task list. Persist the plan in `graph.State` / session state so it survives
  checkpoints and resume (leverage `storage.Storage` checkpoint path). Surface plan updates as
  stream events via the existing `stream.Broker`.
- **Acceptance:** An agent given a multi-step task writes a plan, marks steps done as it
  progresses, and the plan is visible in session state after a resume. Example demonstrates it.
- **Depends on:** none.
- **Tests:** table-driven tool tests (`plan_test.go`); a resume test asserting plan persists
  across a checkpoint (mirror `engine/graph/runner_durability_test.go`).

---

### WC-A-002 — Virtual filesystem for context offloading
- [ ] **Status:** TODO
- **Problem:** Intermediate work (research notes, drafts, tool output) is stuffed into the
  context window, blowing the token budget on long runs. DeepAgents offloads to a virtual FS
  and pages content back in on demand.
- **Location:** new `sdk/harness/vfs.go` with a `VFS` interface (`Read/Write/List/Delete`);
  a session-scoped, tenant-scoped backing store implemented over `storage.Storage`
  (respect `storage/tenant.go` `TenantFromContext`). Built-in tools `fs_write`/`fs_read`/`fs_ls`
  in `engine/tool/builtins/`.
- **Action:** Give the agent scratch-space tools that write to a per-session VFS instead of the
  prompt. Store large artifacts out of context; inject only summaries/paths. Wire cleanup to
  session lifecycle and retention (align with `PLAN.md` P1-012 retention).
- **Acceptance:** An agent can write a large artifact, continue with a small context, and read it
  back later by path. Token usage on a long task is materially lower than without VFS
  (benchmark shows the delta). Two tenants never see each other's files.
- **Depends on:** none (parallel with WC-A-001).
- **Tests:** VFS round-trip + tenant-isolation tests; a benchmark comparing context size
  with/without offloading.

---

### WC-A-003 — Context-isolated & dynamic subagents
- [ ] **Status:** TODO
- **Problem:** `AddSubAgent` (agent.go:170) attaches subagents at build time and does not give
  each a *fresh, isolated context* — the parent's window is shared, defeating the point.
  DeepAgents' June-2026 "dynamic subagents" also create subagents *at runtime* per task.
- **Location:** `sdk/agent/agent.go` (subagent invocation path), `sdk/team/` (reuse
  `handoff.go`, `coordinator.go`, `swarm.go` orchestration), new `sdk/harness/subagent.go`.
- **Action:** (1) A `spawn_subagent` tool that runs a sub-task in a **fresh context** and returns
  only the result to the parent (context isolation). (2) Support **dynamic** creation: subagent
  role/tools/prompt chosen at runtime, not just build time. Route subagent runs through the
  durable queue (`engine/queue/`) so they are resumable and can run on other workers.
- **Acceptance:** A parent agent delegates a sub-task; the subagent's intermediate tokens never
  enter the parent context; only the final result returns. A subagent defined at runtime
  executes successfully. Killing the worker mid-subagent lets another worker resume it.
- **Depends on:** WC-A-001 (planning drives delegation), WC-A-002 (subagents share VFS artifacts).
- **Tests:** context-isolation assertion (parent messages exclude subagent internals);
  dynamic-spawn test; `-race` stress test for concurrent subagents; queue-resume test.

---

### WC-A-004 — Automatic context compaction
- [ ] **Status:** TODO
- **Problem:** `ContextConfig` (`sdk/agent/context.go`) exists but does not auto-summarize/evict
  as the window fills. Long conversations eventually overflow or truncate silently.
- **Location:** `sdk/agent/context.go`, `sdk/agent/agent.go` (`buildChatRequest` around agent.go:253),
  reuse the real tokenizer from `engine/model/tokenizer.go` (PLAN.md P1-009).
- **Action:** When token usage crosses a configurable threshold, summarize older turns into a
  compact memory (optionally persisted via `WithMemoryManager`) and evict raw turns, keeping the
  plan (WC-A-001) and pinned messages. Make it a policy on `ContextConfig`.
- **Acceptance:** A conversation exceeding the window continues coherently without hard
  truncation; token count stays bounded; pinned content and the active plan are always retained.
- **Depends on:** WC-A-001 (plan is pin-protected), D-001 (recall of summarized memory).
- **Tests:** table-driven compaction-policy tests; a long-conversation test asserting bounded
  tokens and retained pins.

---

### WC-A-005 — "Deep agent" harness preset
- [ ] **Status:** TODO
- **Problem:** Even with the primitives above, wiring them by hand is friction. DeepAgents wins
  on being *batteries-included*. Chronos needs a one-call preset.
- **Location:** new `sdk/harness/preset.go`; builder sugar on `sdk/agent` (e.g.
  `agent.New(...).AsDeepAgent()` or a `harness.NewDeepAgent(cfg)` constructor);
  `examples/deep_agent/main.go`.
- **Action:** Assemble planning (A-001) + VFS (A-002) + subagents (A-003) + compaction (A-004) +
  automatic memory recall (D-001) into a single opinionated, override-able preset with sensible
  default system prompt and tool set.
- **Acceptance:** `harness.NewDeepAgent(...)` produces an agent that plans, offloads, delegates,
  and compacts with zero extra wiring; the example completes a realistic long task
  (e.g. "research topic X and produce a report") end-to-end.
- **Depends on:** WC-A-001, WC-A-002, WC-A-003, WC-A-004, WC-D-001.
- **Tests:** integration test driving the preset through a multi-step task with a mock provider;
  docs page under `website/docs/`.
