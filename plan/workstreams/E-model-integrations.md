# Workstream E — Model Serving & Integrations

> **Wave 3.** Breadth parity. Chronos can't (and shouldn't) out-ecosystem LangChain, but two
> gaps hurt perception: no **live/bidirectional multimodal streaming** (ADK's "live" mode for
> voice/video agents) and no curated **connector suite + clean plugin SDK**. MCP + A2A
> (Workstream B) already buy most integration breadth "for free"; this workstream adds the
> high-value native pieces and the extension ergonomics.

---

### WC-E-001 — Live bidirectional (audio/video) streaming
- [ ] **Status:** TODO
- **Problem:** Streaming today is text-token one-directional (`engine/stream/`, `engine/model`
  `StreamChat`). Voice/multimodal agents need a bidirectional live session (audio/video in,
  audio/text out), which ADK ships.
- **Location:** new `engine/model/live.go` (a `LiveProvider` interface alongside
  `provider.go`/`embeddings.go`); transport over the standardized stream (WC-B-003) / websockets
  in `os/server.go`; a provider impl for at least one live-capable model.
- **Action:** Define a `LiveProvider` interface (open a session, send audio/video frames, receive
  streamed audio/text + tool calls). Implement for one provider. Bridge to the harness so a live
  agent can still plan/use tools. Bound and clean up sessions (reuse P1-008 streaming-hardening
  discipline: ctx-aware sends, no goroutine leaks on disconnect).
- **Acceptance:** A voice round-trip works end-to-end (audio in → tool call → audio out);
  disconnecting the client frees the session/goroutine.
- **Depends on:** WC-B-003 (standardized stream transport).
- **Tests:** live-session lifecycle tests with a fake live provider; disconnect/leak `-race` test;
  `examples/live_voice/`.

---

### WC-E-002 — Curated connector suite + plugin SDK
- [ ] **Status:** TODO
- **Problem:** Beyond the built-in tools (`engine/tool/builtins/`: calculator, file, http, shell,
  sql, websearch, sleep) there's no curated set of the connectors enterprises actually ask for,
  and no clean, documented path for third parties to publish tools/providers/adapters.
- **Location:** `engine/tool/builtins/` (new connectors), `engine/tool/toolkit.go` (grouping),
  a documented plugin contract referencing the key interfaces in `CLAUDE.md`
  (`tool.Definition`, `model.Provider`, `storage.Storage`, `knowledge.Knowledge`); the existing
  `.claude/commands/` scaffolders (`new-tool`, `new-adapter`, `add-embedding-provider`).
- **Action:** (1) Ship a curated connector set (e.g. GitHub, Jira/Atlassian, Slack, Postgres,
  Notion) as toolkits — where possible thin wrappers over MCP servers (WC-B-002) rather than
  bespoke code. (2) Publish a **plugin SDK**: a stable, documented extension surface + a
  registration/discovery mechanism so external tools/providers/adapters drop in without core
  changes. Keep everything behind existing interfaces (no layer breaches).
- **Acceptance:** At least five curated connectors work in an example; a third-party tool can be
  registered and used without editing core packages; docs page + a scaffolder command exist.
- **Depends on:** WC-B-002 (MCP-backed connectors) recommended.
- **Tests:** per-connector unit tests with fakes/httptest; a plugin-registration test proving an
  out-of-tree tool loads; `examples/connectors/`.
