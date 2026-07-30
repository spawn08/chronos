# Workstream B — Interoperability Protocols

> **Wave 1.** Interop is becoming table stakes. Google ADK is betting its ecosystem on the
> **A2A (agent-to-agent) protocol**; MCP is the de-facto tool/context standard; frontends expect
> a standard agent event stream. Chronos today is an MCP *client* only (`engine/mcp/client.go`,
> `adapter.go`) and its SSE event schema (`engine/stream/`) is bespoke. Closing this makes
> Chronos agents interoperate with ADK/LangGraph/DeepAgents agents and with any MCP host.

New package suggested: `engine/interop/` (or `os/interop/` for the served endpoints), with
sub-packages `a2a/`, `mcpserver/`, `agui/`.

---

### WC-B-001 — A2A (agent-to-agent) client + server
- [ ] **Status:** TODO
- **Problem:** Chronos agents cannot call, or be called by, agents from other frameworks. This
  is the interop standard ADK is standardizing the ecosystem around.
- **Location:** new `engine/interop/a2a/` (protocol types + client); served endpoint in
  `os/server.go` route table (`os/server.go:118-217` handler block) behind the auth middleware
  chain (PLAN.md P0-010); reuse `storage/tenant.go` for tenant scoping.
- **Action:** (1) **Server:** expose Chronos agents as A2A endpoints — publish an agent card
  (capabilities/skills from `sdk/skill/`), accept task requests, stream task updates. Back
  long-running tasks with the durable queue (`engine/queue/`) so A2A tasks are resumable.
  (2) **Client:** a `tool.Definition`/adapter that lets a Chronos agent invoke a remote A2A agent
  as a delegated subagent (composes with WC-A-003).
- **Acceptance:** A Chronos agent is discoverable and invocable over A2A by an external client;
  a Chronos agent can delegate a task to an external A2A agent and receive streamed results.
  Auth + tenant scoping enforced on the served side (no cross-tenant task access → 404).
- **Depends on:** WC-A-003 (remote agents are a subagent flavor).
- **Tests:** protocol round-trip with an in-process fake peer; auth/tenant rejection tests;
  `examples/a2a_interop/main.go`.

---

### WC-B-002 — MCP server (expose Chronos tools/agents as MCP)
- [x] **Status:** DONE <!-- done: 2026-07-29 -->
  - **Delivered:** new `engine/interop/mcpserver` package. `Server` exposes tools sourced from a
    `tool.Registry`; a `tools/call` dispatches through `Registry.Execute`, so it automatically
    honors each tool's `Permission`, the approval hook, and the panic-to-error recovery (P0-009).
    `initialize`/`ping`/`tools/list`/`tools/call` implemented; `HandleMessage` is a
    transport-agnostic JSON-RPC 2.0 dispatcher shared by both transports: `ServeStdio`
    (newline-delimited, bounded line size, serialized writes) and `SSEHandler` (HTTP+SSE with the
    `endpoint`→`message` handshake, per-session routing, heartbeat) — the SSE transport
    interoperates with the existing `engine/mcp` SSE client. Safe-by-default: a tool exposed without
    an explicit permission defaults to `PermRequireApproval` (`WithDefaultPermission` to change);
    `PermDeny` tools are never advertised; tool failures/denials/panics return as MCP `isError`
    results, not transport errors. Example `examples/mcp_server/main.go` (runnable over stdio);
    docs added to `website/docs/guides/mcp.md`. Tests: protocol conformance + permission/approval
    gate + panic containment via `HandleMessage`; a `ServeStdio` frame round-trip; and a full
    conformance round-trip driving the **real `engine/mcp` SSE client** against `SSEHandler` over
    `httptest` (initialize → tools/list → tools/call, plus approval-denied surfaced as an error).
    `-race` green.
  - **Review gates:** design-pattern-reviewer APPROVE ("thin protocol adapter over the engine's single
    enforcement path"); code-quality-auditor findings fixed in-branch — `Expose` copies rather than
    mutating the caller's Definition; error frames now emit `"id":null` (dropped `omitempty` on the
    response id) with a test; a notification is silent even when malformed; the no-op stdio write
    mutex was removed; the oversized-frame transport exception is documented; `marshalError` is a
    package function; SSE gets its own `maxBodyBytes` constant.
- **Problem:** Chronos consumes MCP servers but cannot *be* one. Other hosts (Claude Desktop,
  IDEs, ADK, LangGraph) cannot consume Chronos tools/agents.
- **Location:** new `engine/interop/mcpserver/`; source tools from `engine/tool/registry.go`
  (the `Registry`); support stdio + SSE transports (mirror the client transports in
  `engine/mcp/`, and finish parity with the SSE work referenced in `docs/mcp-transports.md`).
- **Action:** Implement the MCP server side: advertise the tool registry's definitions,
  handle `tools/call` by dispatching through the registry (honoring `Permission` and the
  approval hook — default remote-exposed tools to `PermRequireApproval`, per the P1-019
  precedent). Optionally expose skills/prompts.
- **Acceptance:** An external MCP host lists and calls a Chronos-exposed tool; permissioned tools
  require approval; a panicking tool fails only its call (recover already in place, PLAN.md P0-009).
- **Depends on:** none.
- **Tests:** MCP server conformance round-trip over stdio; permission/approval path test;
  `examples/mcp_server/main.go`.

---

### WC-B-003 — AG-UI standard agent event stream
- [x] **Status:** DONE <!-- done: 2026-07-30 -->
  - **Delivered:** new `os/interop/agui` package — a translation layer over the existing
    `stream.Broker` and its per-session SSE routing, served at `GET /api/agui/stream` **alongside**
    the unchanged native `/api/events/stream`. `Translator` maps native events to the AG-UI protocol
    (`RUN_STARTED`/`RUN_FINISHED`/`RUN_ERROR`, `STEP_STARTED`/`STEP_FINISHED`,
    `TOOL_CALL_START`/`ARGS`/`END`/`RESULT` with a correlated per-run tool-call id, `STATE_SNAPSHOT`,
    and `CUSTOM` for plan updates [WC-A-001] and interrupts). Heterogeneous broker payloads (the
    runner's `graph.StreamEvent` struct vs the agent's `map[string]any`) are normalized via a JSON
    round-trip. `RUN_STARTED` is emitted on connect (not lazily) so a client sees a live lifecycle
    immediately; per-session isolation and heartbeat are inherited from the broker. `agui.Handler`
    is embeddable on any mux; `agui.Translator` is transport-agnostic. Example
    `examples/agui_stream/main.go` (key-free), docs `website/docs/guides/agui.md`. Tests:
    table-driven translator mapping (both payload shapes), tool-call id correlation, and live SSE
    tests over `httptest` (full translated run + per-session isolation). `-race` green.
  - **Review-driven hardening (both gates):** the reviews surfaced real upstream gaps that this item
    now fixes so the "streaming tokens → tool calls → plan → completion, per-session isolated"
    acceptance actually holds. In `sdk/agent`: model/tool/error events now route to the session topic
    (`a.publish(ctx, …)` using `storage.SessionFromContext`) instead of broadcasting — closing a
    cross-session leak on both the native and AG-UI streams; tool events carry the model tool-call id;
    the assistant's text is published (final content on the blocking path, token `model_delta`s on the
    streaming path). The translator gains `TEXT_MESSAGE_*` (streamed and one-shot), id-based tool-call
    correlation, and an orphan-guard that synthesizes a `TOOL_CALL_START` when a result arrives with no
    seen call (BLOCK-Q01). The handler honors the broker's configured heartbeat (`stream.Broker.Heartbeat()`)
    instead of hardcoding it. New `stream.EventModelDelta`.
- **Problem:** The SSE event schema (`engine/stream/stream.go`, `modes.go`) is Chronos-specific,
  so frontends must be hand-built per app. A standard agent-UI event protocol lets any compatible
  frontend render Chronos runs (tokens, tool calls, plan updates, state transitions, HITL prompts).
- **Location:** new `os/interop/agui/` mapping layer over the existing `stream.Broker` and
  per-session SSE routing (PLAN.md P1-015); served on `os/server.go`.
- **Action:** Define/adopt a standard event schema and translate the existing broker events
  (token deltas, tool start/stop, plan updates from WC-A-001, node transitions, interrupts) into
  it. Keep per-session/tenant routing and heartbeat from P1-015. Do not break the existing SSE
  consumers — add the standardized stream alongside.
- **Acceptance:** A reference frontend renders a full run (streaming tokens → tool calls → plan
  updates → HITL approval → completion) against the standardized stream with no Chronos-specific
  glue; events are per-session isolated.
- **Depends on:** WC-A-001 (plan events are part of the stream).
- **Tests:** event-mapping table tests; per-session isolation test (reuse `os/server_sse_test.go`
  patterns); `examples/agui_stream/`.
