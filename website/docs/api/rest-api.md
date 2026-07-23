---
title: "REST API Reference"
sidebar_label: "REST API"
---


This is the complete HTTP reference for the [ChronosOS control plane](/guides/server)
started with `chronos serve`. The default base URL is `http://localhost:8420`.

:::tip Live, interactive reference
The server ships an interactive **Swagger UI at [`/swagger/`](http://localhost:8420/swagger/)**
and the raw **OpenAPI 3 document at [`/swagger/doc.json`](http://localhost:8420/swagger/doc.json)**.
The generated spec is the authoritative, always-in-sync source of truth — use it
to try requests in the browser or to generate clients. This page mirrors it in
prose.
:::

## Conventions

- All request and response bodies are JSON (`Content-Type: application/json`),
  except `/metrics` (Prometheus text) and `/api/events/stream` (SSE).
- Timestamps are RFC 3339 strings.
- **Auth requirement** is shown per endpoint (`viewer+` = read, `user+` =
  mutating). It is enforced only when both an auth mode **and** RBAC are enabled
  (`CHRONOS_AUTH=jwt|apikey` **and** `CHRONOS_RBAC=true` — both default off; see
  [Authentication](/guides/authentication#roles--rbac)). With auth on but RBAC
  off, any valid credential may call any `/api/*` route. Health, metrics, and
  Swagger endpoints are **always public**.
- When auth is enabled, results are automatically scoped to the caller's tenant.

### Common status codes

| Code | Meaning |
|------|---------|
| `200 OK` | Success |
| `201 Created` | Resource created |
| `400 Bad Request` | Malformed query/body or missing required parameter |
| `401 Unauthorized` | Missing/invalid credentials (auth enabled) |
| `403 Forbidden` | Authenticated but role lacks permission |
| `404 Not Found` | Resource does not exist (or belongs to another tenant) |
| `429 Too Many Requests` | Rate limit or per-key quota exceeded |
| `500 Internal Server Error` | Unhandled server error (panic-recovered) |

---

## Health

### `GET /healthz` · `GET /health` · `GET /health/live` · `GET /health/ready`

Liveness and readiness probes. **Always unauthenticated.**

- `/healthz`, `/health`, `/health/live` — the process is up.
- `/health/ready` — dependencies (storage) are reachable and the server is ready
  to accept traffic.

**Response** `200 OK`

```json
{ "status": "ok" }
```

```bash
curl http://localhost:8420/health/ready
```

See [Health & readiness](/guides/server#health--readiness) for probe semantics.

---

## Metrics

### `GET /metrics`

Prometheus-format metrics. **Always unauthenticated.**

**Response** `200 OK` (`text/plain; version=0.0.4`)

```bash
curl http://localhost:8420/metrics
```

---

## Sessions

### `GET /api/sessions`

List sessions, most recent first.

**Auth:** viewer+

| Query param | Type | Default | Description |
|-------------|------|---------|-------------|
| `agent_id` | string | — | Filter to one agent (optional) |
| `limit` | int | `50` | Page size |
| `offset` | int | `0` | Page offset |

**Response** `200 OK`

```json
{
  "sessions": [
    {
      "id": "sess-8f2a",
      "agent_id": "support-bot",
      "tenant_id": "acme",
      "created_at": "2026-07-23T10:15:04Z",
      "updated_at": "2026-07-23T10:18:22Z"
    }
  ]
}
```

```bash
curl "http://localhost:8420/api/sessions?agent_id=support-bot&limit=20&offset=0"
```

---

### `GET /api/sessions/state`

Fetch the **latest checkpoint** for a session.

**Auth:** viewer+

| Query param | Type | Required | Description |
|-------------|------|:--------:|-------------|
| `session_id` | string | yes | Session identifier |

**Response** `200 OK`

```json
{
  "session_id": "sess-8f2a",
  "checkpoint_id": "ckpt-14",
  "node_id": "await_approval",
  "state": { "ticket_id": 4821, "status": "pending" },
  "seq_num": 14
}
```

```bash
curl "http://localhost:8420/api/sessions/state?session_id=sess-8f2a"
```

**Errors:** `400` if `session_id` is missing; `404` if the session has no
checkpoint or belongs to another tenant.

---

### `POST /api/sessions/state`

Merge a partial state patch into the latest checkpoint. This powers the
**edit-then-resume** flow — an operator corrects state before the graph resumes.

**Auth:** user+

| Query param | Type | Required | Description |
|-------------|------|:--------:|-------------|
| `session_id` | string | yes | Session identifier |

**Request body**

```json
{ "state": { "status": "approved", "approver": "ops@acme.com" } }
```

The `state` object is **shallow-merged** into the current checkpoint state;
existing keys not present in the patch are preserved.

**Response** `200 OK`

```json
{
  "session_id": "sess-8f2a",
  "checkpoint_id": "ckpt-15",
  "node_id": "await_approval",
  "state": { "ticket_id": 4821, "status": "approved", "approver": "ops@acme.com" },
  "seq_num": 15
}
```

```bash
curl -X POST "http://localhost:8420/api/sessions/state?session_id=sess-8f2a" \
  -H "Content-Type: application/json" \
  -d '{"state":{"status":"approved","approver":"ops@acme.com"}}'
```

---

## Traces

### `GET /api/traces`

Fetch trace spans for a session (for debugging and observability).

**Auth:** viewer+

| Query param | Type | Required | Description |
|-------------|------|:--------:|-------------|
| `session_id` | string | yes | Session identifier |

**Response** `200 OK`

```json
{
  "traces": [
    {
      "span_id": "span-01",
      "parent_id": "",
      "name": "agent.chat",
      "start": "2026-07-23T10:15:04.001Z",
      "end": "2026-07-23T10:15:06.402Z",
      "attributes": { "model": "gpt-4o", "tokens": 512 }
    }
  ]
}
```

```bash
curl "http://localhost:8420/api/traces?session_id=sess-8f2a"
```

---

## Events (SSE)

### `GET /api/events/stream`

Long-lived **server-sent events** stream of graph/run events.
`Content-Type: text/event-stream`. The connection stays open until the client
disconnects (the server clears the write deadline for this route).

**Auth:** viewer+

| Query param | Type | Required | Description |
|-------------|------|:--------:|-------------|
| `session` | string | no | Scope to one session. Omit for the **firehose** (all sessions on this replica). |

**Response** `200 OK` — a sequence of SSE `data:` frames:

```
data: {"type":"node_start","session":"sess-8f2a","node":"classify"}

data: {"type":"node_end","session":"sess-8f2a","node":"classify","status":"ok"}

data: {"type":"completed","session":"sess-8f2a"}
```

```bash
# One session
curl -N "http://localhost:8420/api/events/stream?session=sess-8f2a"

# Firehose
curl -N "http://localhost:8420/api/events/stream"
```

See [Streaming & SSE](/guides/streaming) for event types and the broker.

---

## Approvals

Human-in-the-loop approvals. Available when the server is started with an
approval service (`WithApproval`).

### `GET /api/approval/pending`

List pending approval requests awaiting a decision.

**Auth:** user+

**Response** `200 OK`

```json
{
  "pending": [
    {
      "id": "appr-77",
      "session_id": "sess-8f2a",
      "node_id": "await_approval",
      "prompt": "Refund $250 to customer?",
      "requested_at": "2026-07-23T10:16:00Z"
    }
  ]
}
```

```bash
curl http://localhost:8420/api/approval/pending
```

### `POST /api/approval/respond`

Approve or reject a pending request, unblocking the paused graph.

**Auth:** user+

**Request body**

```json
{ "id": "appr-77", "approved": true, "comment": "Within policy" }
```

**Response** `200 OK`

```json
{ "id": "appr-77", "status": "approved" }
```

```bash
curl -X POST http://localhost:8420/api/approval/respond \
  -H "Content-Type: application/json" \
  -d '{"id":"appr-77","approved":true,"comment":"Within policy"}'
```

---

## Schedules

Cron-triggered agent runs. Available when the server is started with a scheduler
(`WithScheduler`).

### `GET /api/schedules`

List all schedules.

**Auth:** viewer+

**Response** `200 OK`

```json
{
  "schedules": [
    {
      "id": "sched-1",
      "agent_id": "digest-bot",
      "cron_expr": "0 9 * * *",
      "input": { "topic": "overnight incidents" },
      "new_session": true,
      "next_run": "2026-07-24T09:00:00Z"
    }
  ]
}
```

```bash
curl http://localhost:8420/api/schedules
```

### `POST /api/schedules`

Create a schedule.

**Auth:** user+

**Request body**

```json
{
  "agent_id": "digest-bot",
  "cron_expr": "0 9 * * *",
  "input": { "topic": "overnight incidents" },
  "new_session": true
}
```

| Field | Type | Description |
|-------|------|-------------|
| `agent_id` | string | Agent to run |
| `cron_expr` | string | Standard 5-field cron expression |
| `input` | object | State/input passed to the agent on each fire |
| `new_session` | bool | Start a fresh session per fire (`true`) or reuse one (`false`) |

**Response** `201 Created`

```json
{
  "id": "sched-2",
  "agent_id": "digest-bot",
  "cron_expr": "0 9 * * *",
  "input": { "topic": "overnight incidents" },
  "new_session": true,
  "next_run": "2026-07-24T09:00:00Z"
}
```

```bash
curl -X POST http://localhost:8420/api/schedules \
  -H "Content-Type: application/json" \
  -d '{"agent_id":"digest-bot","cron_expr":"0 9 * * *","input":{"topic":"overnight incidents"},"new_session":true}'
```

### `GET /api/schedules/{id}`

Fetch a single schedule by ID.

**Auth:** viewer+

**Response** `200 OK` — same shape as a list item. `404` if not found.

```bash
curl http://localhost:8420/api/schedules/sched-2
```

### `DELETE /api/schedules/{id}`

Delete a schedule.

**Auth:** user+

**Response** `200 OK`

```json
{ "id": "sched-2", "status": "deleted" }
```

```bash
curl -X DELETE http://localhost:8420/api/schedules/sched-2
```

### `GET /api/schedules/{id}/history`

Fetch past fires for a schedule.

**Auth:** viewer+

**Response** `200 OK`

```json
{
  "history": [
    {
      "schedule_id": "sched-2",
      "session_id": "sess-9c1d",
      "fired_at": "2026-07-23T09:00:00Z",
      "status": "completed"
    }
  ]
}
```

```bash
curl http://localhost:8420/api/schedules/sched-2/history
```

---

## Swagger / OpenAPI

### `GET /swagger/`

Interactive Swagger UI. **Always public.**

### `GET /swagger/doc.json`

The OpenAPI 3 document backing the UI — feed it to client generators. **Always
public.**

:::note Disabling Swagger
Both Swagger routes are served only when `CHRONOS_SWAGGER=true` (the default).
Set `CHRONOS_SWAGGER=false` (SDK: `chronosos.WithSwagger(false)`) to remove them
entirely — recommended on hardened production control planes, since they bypass
auth. When disabled, both paths return `404`.
:::

```bash
curl http://localhost:8420/swagger/doc.json | jq '.paths | keys'
```

---

## See also

- [ChronosOS Server](/guides/server) — starting and operating the control plane
- [Authentication & Authorization](/guides/authentication) — securing these endpoints
