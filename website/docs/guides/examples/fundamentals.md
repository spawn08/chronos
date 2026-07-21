---
title: "Fundamentals"
---


# Fundamentals

No-API-key examples that use mock providers and SQLite — they compile and run instantly. Start here to learn the agent builder, tools, guardrails, graph patterns, memory, and streaming.

```bash
go run ./examples/<name>/
```

See the [Examples overview](../examples.md) for the full index and provider setup.

---

## quickstart

Minimal agent with SQLite storage and a 3-node StateGraph (greet → classify → respond).

```bash
go run ./examples/quickstart/
```

**Demonstrates:** Agent builder, SQLite storage, graph nodes, `Run()` method.

---

## chat_with_tools

Agent with tool definitions: direct tool execution and model-aware tool passing.

```bash
go run ./examples/chat_with_tools/
```

**Demonstrates:**
- Calculator tool with expression parsing
- Geography lookup tool
- Direct tool execution via `agent.Tools.Execute()`
- Tool definitions automatically passed to model in `Chat()` requests
- JSON Schema tool parameter definitions

---

## tools_and_guardrails

Tool registry with three permission levels (allow, deny, require_approval) and input/output guardrails.

```bash
go run ./examples/tools_and_guardrails/
```

**Demonstrates:**
- Registering tools with handlers and JSON Schema parameters
- `tool.PermAllow` — auto-executed tools (calculator, weather)
- `tool.PermDeny` — blocked tools (delete_database)
- `tool.PermRequireApproval` — tools requiring human approval (send_email)
- `BlocklistGuardrail` — blocks inputs containing prohibited terms
- `MaxLengthGuardrail` — limits output length
- Approval handler callback

---

## graph_patterns

StateGraph patterns: conditional edges, interrupt nodes (human-in-the-loop), stream events, and multi-path routing.

```bash
go run ./examples/graph_patterns/
```

**Demonstrates:**
- `AddConditionalEdge` — dynamic routing based on state (e.g., order validation)
- `AddInterruptNode` — pauses execution for human approval with checkpoint
- `graph.NewRunner` + `runner.Stream()` — real-time execution events
- Multi-path graphs with convergence (support ticket triage)
- Checkpoint persistence for resume

---

## memory_and_sessions

Short-term and long-term memory APIs, plus multi-turn persistent sessions.

```bash
go run ./examples/memory_and_sessions/
```

**Demonstrates:**
- `memory.NewStore` — short-term (session-scoped) and long-term (cross-session) memory
- `SetShortTerm`, `SetLongTerm`, `Get`, `ListShortTerm`, `ListLongTerm`
- `ChatWithSession` — persistent multi-turn conversations
- Session lifecycle: creation, event ledger, listing
- Multiple sessions per agent

---

## streaming_sse

Event broker for real-time observability: pub/sub, graph stream events, and SSE HTTP handler.

```bash
go run ./examples/streaming_sse/
```

**Demonstrates:**
- `stream.NewBroker` — publish/subscribe event system
- Multiple subscribers receiving the same events
- Graph runner stream events (`node_start`, `node_end`, `edge_transition`, `completed`)
- `SSEHandler` — HTTP endpoint for Server-Sent Events
- Integration pattern for real-time dashboards
