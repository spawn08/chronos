# Plan Conventions — how agents execute this roadmap

This roadmap is executed by **multiple agents across multiple sessions**. These conventions
keep the work parallelizable, resumable, and safe. They mirror the process that delivered
`../PLAN.md` Waves 1–3.

## Item anatomy

Every item in a workstream file has this shape:

```
### WC-<WS>-<NNN> — <Title>
- [ ] **Status:** TODO | IN-PROGRESS | REVIEW | DONE
- **Problem:** what is missing / wrong today, and why it matters competitively.
- **Location:** real files/symbols to touch (verified against the tree).
- **Action:** concrete implementation steps.
- **Acceptance:** objective, testable "done when …" criteria.
- **Depends on:** other WC-* item IDs (or "none").
- **Tests:** the specific tests to ship.
```

- **ID scheme:** `WC-<workstream letter>-<3-digit>` (e.g. `WC-A-001`). IDs are stable — never renumber.
- Keep items **additive** where possible: prefer new packages/interfaces over signature changes to
  shipped public APIs. If a signature must change, note affected callers in the Action.

## Status lifecycle

`TODO → IN-PROGRESS → REVIEW → DONE`

- On starting: set `IN-PROGRESS`, add your entry to [`STATUS.md`](STATUS.md) (item, agent/owner, branch).
- On opening a PR: set `REVIEW`.
- On merge with acceptance criteria met: tick `- [x]`, set `DONE`, append `<!-- done: YYYY-MM-DD -->`,
  and update `STATUS.md`. **Do these in the same PR as the code** so the plan never drifts from `main`.

## Branch & commit

- One branch per item or per tightly-coupled item group: `plan/wc-<ws>-<short-slug>`
  (e.g. `plan/wc-a-planning-tool`). Mirrors the existing `plan/p1-wave2` style.
- Conventional commits, matching repo history: `feat(harness): add planning todo tool (WC-A-001)`.
- The repo auto-commits via a hook and enforces pre-push checks — **run them before pushing**:
  ```bash
  ./scripts/pre-push-check.sh          # full: format + vet + lint + build + test
  ./scripts/pre-push-check.sh --quick  # fast iteration
  ```

## Review gates (required)

Every item passes **two adversarial review agents** before merge, same as Waves 1–3:

- **design-pattern-reviewer** — SOLID, layer rules, coupling, pattern integrity.
- **code-quality-auditor** — dead code, duplication, error handling, complexity.

Fix all findings in-branch. Record the gate outcome in the PR description.

## Layer rules (do not breach)

Import direction is strictly top-down; lower layers never import higher ones:

```
os/  ──►  engine/  ──►  storage/            (control plane → runtime → persistence)
sdk/ ──►  engine/  ──►  storage/            (SDK → runtime → persistence)
```

- Never import `engine`/`os` from `storage`. Never import `os` from `engine` or `sdk`.
- Do **not** `import "os"` (the stdlib) in library packages — only `cli/` and `examples/`.
- New cross-cutting contracts go behind an **interface** in the owning package, per the
  interface-segregation pattern in `CLAUDE.md`.

## Coding conventions (from CLAUDE.md — non-negotiable)

- Package comment on every package; `context.Context` first arg on all I/O methods.
- Wrap every error: `fmt.Errorf("what: %w", err)`. No `panic` for recoverable errors.
- Constructors `New(...)`; **no `init()`**, no package-level mutable state.
- JSON tags on all exported struct fields.

## Testing bar (every item)

- Table-driven tests in `*_test.go`, same package.
- Any concurrency-touching code ships a `-race` stress test.
- New hot paths ship a `Benchmark*`.
- New storage/protocol behavior: unit-tested with fakes; full round-trips gated behind env
  vars or `testing.Short()` (follow the `REDISVECTOR_ADDR` / `testing.Short()` precedent).
- Coverage must not regress. Do **not** add `_squeeze/_boost/_max` coverage-padding tests
  (`PLAN.md` P2-006 flagged ~110 of these as debt — write meaningful tests instead).

## Per-item deliverables checklist

- [ ] Code + tests pass `./scripts/pre-push-check.sh`
- [ ] `-race` green for concurrency changes
- [ ] A runnable `examples/<feature>/main.go` when the item adds a user-facing capability
- [ ] Docs: a page under `website/docs/` or `docs/` for user-facing features
- [ ] Both review-agent gates passed, findings fixed
- [ ] Checkbox flipped + `STATUS.md` updated in the same PR

## Definition of "world-class" (exit criteria for this roadmap)

Shipped: harness parity (Workstream A, retired from the active roadmap), interop (A2A +
MCP-server + AG-UI stream, Workstream B), and automatic cross-session semantic memory
(Workstream D). Remaining:

1. **Loved DX** — trace→dataset→eval→gate loop exists; a visual debugger and one-command
   deploy do not yet (Workstream C).
2. **Enterprise lead** — per-tenant budgets/policy/compliance-export shipped and
   self-hostable (Workstream F).
