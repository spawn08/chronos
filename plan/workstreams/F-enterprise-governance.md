# Workstream F — Enterprise Governance

> **Wave 3. Chronos's chance to LEAD, not follow.** No competitor offers a self-hostable,
> per-tenant governance layer — budgets, policy enforcement, and compliance export as a
> first-class control-plane product. Chronos already has the pieces: cost/rate-limit hooks
> (`engine/hooks/{cost,ratelimit}.go`, PLAN.md P1-010), per-key/per-tenant quotas + budgets
> (`os/auth/`, PLAN.md P2-003), tenant scoping (`storage/tenant.go`, P2-002), and audit logging
> (`os/trace/`). This workstream productizes them into a governance layer enterprises buy for.

---

### WC-F-001 — Per-tenant budget & quota policy engine
- [ ] **Status:** TODO
- **Problem:** Budgets/quotas exist as scattered hooks and auth fields, not as a coherent,
  centrally-configured, centrally-enforced policy with graceful degradation when limits hit.
- **Location:** new `os/governance/` (policy definition + evaluation), enforced via
  `engine/hooks/cost.go` + `engine/hooks/ratelimit.go` (the execution-path hooks) and the
  distributed rate limiter (PLAN.md P1-016); tenant context from `storage/tenant.go`; principal
  from `os/auth` `UserClaims.TenantID`.
- **Action:** Define per-tenant/per-key policies: token budgets, cost ceilings ($/day/month),
  request quotas, concurrency caps. Enforce on the execution path (reject/park/degrade — reuse
  P1-005 admission control) rather than after the fact. Persist counters (tenant-scoped) so
  enforcement is correct across replicas. Emit governance events/metrics (P1-017).
- **Acceptance:** A tenant that exceeds its monthly cost ceiling is throttled/rejected with a
  clear error, enforced consistently across ≥2 replicas; usage counters are accurate under
  concurrency; policies are hot-reloadable.
- **Depends on:** none (builds on shipped hooks + tenant + rate limiter).
- **Tests:** policy-evaluation table tests; `-race` counter test; a two-replica enforcement test
  (shared store) gated behind env/`testing.Short()`; `examples/governance/`.

---

### WC-F-002 — Model allow-lists & data-residency/PII policy
- [ ] **Status:** TODO
- **Problem:** Enterprises must constrain which models/regions a tenant may use and enforce
  PII/data-residency rules centrally. `examples/data_residency/` and `engine/guardrails/` show
  intent, but there's no central policy enforcing it.
- **Location:** `os/governance/` (policy), enforced via `engine/guardrails/` (input/output
  `Guardrail`), model selection in `engine/model/` (provider gating / `fallback.go`), and the
  tenant context.
- **Action:** Per-tenant policy for: allowed model providers/models, allowed regions/residency,
  and required PII guardrails (redaction/blocking) on inputs and outputs. Enforce at model-select
  time (deny disallowed models) and via guardrail chain (block/redact). Log every policy decision
  to the audit trail.
- **Acceptance:** A request routed to a disallowed model or region is denied with a clear reason;
  configured PII is redacted/blocked on input and output; every decision is auditable.
- **Depends on:** WC-F-001 (shares the policy engine).
- **Tests:** allow-list deny tests; residency-routing tests; PII redaction guardrail tests;
  audit-entry assertions.

---

### WC-F-003 — Compliance-grade audit & export
- [ ] **Status:** TODO
- **Problem:** Audit logging exists (`os/trace/`, `storage.Storage` audit logs) but is not
  packaged for SOC2/GDPR needs: tamper-evident, complete, and exportable. This is a real
  enterprise buying criterion competitors don't address self-hosted.
- **Location:** `os/trace/` + `storage.Storage` audit-log path; new `os/governance/audit.go`
  (export + integrity); CLI export command in `cli/cmd/`.
- **Action:** Ensure every security-relevant action (auth, tool calls, approvals, policy
  decisions, data access) is audited with tenant + principal + correlation id (P1-017). Add
  tamper-evidence (hash-chained entries) and an export API/CLI (JSON/CSV) scoped by tenant and
  time range. Document a data-subject-access / right-to-erasure procedure over the retention path
  (P1-012).
- **Acceptance:** A tenant admin exports a complete, tamper-evident audit trail for a time range;
  altering a past entry is detectable; a right-to-erasure request removes a subject's data while
  preserving audit integrity.
- **Depends on:** WC-F-001, WC-F-002 (their decisions must be audited).
- **Tests:** hash-chain integrity tests (including tamper detection); export round-trip tests;
  erasure test preserving chain validity; `examples/compliance_export/`.
