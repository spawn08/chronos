# Workstream B — Interoperability Protocols — COMPLETE

> **Wave 1.** Chronos exposes and consumes A2A, is an MCP server (not just a client), and
> speaks a standard AG-UI event stream. Full delivery history: `plan/STATUS.md` progress log
> and the `plan/wc-b-*` branches.

---

### WC-B-001 — A2A (agent-to-agent) client + server
- [x] **Status:** DONE <!-- done: 2026-07-31 -->
- **Delivered:** `sdk/protocol/a2a` gained a `TaskStore` seam (in-memory + durable, queue-backed)
  behind an HTTP server (agent card, create/get/cancel, SSE task stream) and a streaming client
  (`NewRemoteAgentTool`). Served via `os/server.go` `WithA2A` behind the auth/tenant chain.
  Example `examples/a2a_interop/`, docs `website/docs/guides/a2a.md`.

---

### WC-B-002 — MCP server (expose Chronos tools/agents as MCP)
- [x] **Status:** DONE <!-- done: 2026-07-29 -->
- **Delivered:** `engine/interop/mcpserver` exposes `engine/tool.Registry` over stdio + SSE
  (JSON-RPC 2.0: initialize/ping/tools/list/tools/call), honoring tool `Permission` and the
  approval/panic-recovery path by construction. Example `examples/mcp_server/`, docs
  `website/docs/guides/mcp.md`.

---

### WC-B-003 — AG-UI standard agent event stream
- [x] **Status:** DONE <!-- done: 2026-07-30 -->
- **Delivered:** `os/interop/agui` translates the native `stream.Broker` events (per-session,
  P1-015) into the AG-UI protocol (run/step lifecycle, tool-call correlation, state snapshots,
  plan/interrupt custom events), served at `GET /api/agui/stream` alongside the native stream.
  Example `examples/agui_stream/`, docs `website/docs/guides/agui.md`.
