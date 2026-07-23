---
title: "The ChronosOS Server (chronos serve)"
sidebar_label: "ChronosOS Server"
---


`chronos serve` starts **ChronosOS** — the Chronos control plane. It is a single,
self-contained HTTP server that exposes everything you need to operate agents in
production: a REST API over sessions, checkpoints, traces, schedules and
human-in-the-loop approvals; a live server-sent-events (SSE) firehose; Prometheus
metrics; health/readiness probes; and an interactive **Swagger UI**.

The control plane is **stateless** — all durable state lives in the configured
[storage backend](/guides/storage). You can therefore run many replicas behind a
load balancer, and any replica can serve any request.

:::note At a glance
- Default listen address: `:8420`
- REST API mounted under `/api/*`
- Swagger UI at `/swagger/`, OpenAPI JSON at `/swagger/doc.json`
- Auth is **opt-in** (default: none) — see [Authentication & Authorization](/guides/authentication)
- Hardened by default: timeouts, body limits, panic recovery, CORS, rate limiting, graceful shutdown
:::

## Starting the server

### From the CLI

```bash
# Start on the default address (:8420)
chronos serve

# Bind a custom address
chronos serve :9000
chronos serve 127.0.0.1:8420
```

The server reads its agent roster and storage configuration from
`.chronos/agents.yaml` (see [Configuration](/getting-started/configuration)). On
start it logs the resolved listen address, the storage backend, and whether auth
is enabled.

Stop it with `Ctrl-C` (SIGINT) or `SIGTERM` — the server drains in-flight
requests before exiting (see [Graceful shutdown](#graceful-shutdown)).

### From the SDK

For embedding the control plane in your own binary, use the `os/chronosos`
package. The functional-options constructor lets you enable auth, tune the
hardening defaults, and attach a scheduler, approval service, or a store-backed
rate limiter:

```go
package main

import (
    "context"
    "log"

    "github.com/spawn08/chronos/os/chronosos"
    "github.com/spawn08/chronos/os/auth"
    "github.com/spawn08/chronos/storage/adapters/postgres"
)

func main() {
    ctx := context.Background()

    store, err := postgres.New(ctx, "postgres://user:pass@db:5432/chronos")
    if err != nil {
        log.Fatal(err)
    }

    srv := chronosos.NewWithOptions(":8420", store,
        // Opt in to JWT auth (see the Authentication guide)
        chronosos.WithJWTAuth(auth.JWTConfig{
            Secret:   "…",          // HS256 shared secret
            Issuer:   "https://issuer.example.com",
            Audience: "chronos",
        }),
        // Store-backed limiter so limits are shared across replicas
        chronosos.WithRateLimiter(store),
    )

    if err := srv.ListenAndServe(ctx); err != nil {
        log.Fatal(err)
    }
}
```

The simplest form, `chronosos.New(addr, store)`, applies all hardening defaults
with **no authentication** — suitable for a trusted network or local development.

## Configuration & options

Every hardening behaviour has a sensible default and a corresponding SDK option.

| Option | Default | Purpose |
|--------|---------|---------|
| `WithJWTAuth(auth.JWTConfig{…})` | off | Enable JWT bearer-token auth (HS256 / RS256 / JWKS). See [Authentication](/guides/authentication). |
| `WithAPIKeyAuth(auth.APIKeyConfig{…})` | off | Enable `X-Api-Key` header auth with optional per-key quotas. |
| `WithRBAC(true)` | off | Enforce roles on `/api/*` — reads need `viewer`, mutations need `user`. Effective only with auth enabled (`CHRONOS_RBAC`). |
| `WithSwagger(false)` | Swagger **on** | Disable the Swagger UI and OpenAPI spec (`CHRONOS_SWAGGER`). |
| `WithCORS(cfg)` / `WithoutCORS()` | CORS **on** | Configure or disable the CORS middleware. |
| `WithRateLimit(cfg)` / `WithoutRateLimit()` | rate limit **on** | Configure or disable request rate limiting. |
| `WithRateLimiter(store)` | in-memory | Use a **store-backed** limiter so limits are shared across replicas. |
| `WithTimeouts(read, readHeader, write, idle)` | see below | Override the server timeouts. |
| `WithMaxBodyBytes(n)` | 1 MiB | Maximum request body size (also caps header size). |
| `WithScheduler(sched)` | off | Attach a cron scheduler to enable the `/api/schedules` endpoints. |
| `WithApproval(svc)` | off | Attach a human-in-the-loop approval service for `/api/approval/*`. |

### Hardening defaults

Applied automatically by both `chronos serve` and `chronosos.New`:

| Setting | Default |
|---------|---------|
| Read timeout | 30s |
| Read-header timeout | 10s |
| Write timeout | 30s (cleared for SSE streams) |
| Idle timeout | 120s |
| Max header + body size | 1 MiB |
| Panic recovery | on (returns `500`, logs stack) |
| CORS | on |
| Rate limiting | on |
| Graceful shutdown | on (SIGTERM / SIGINT) |

:::tip Long-lived streams
The `/api/events/stream` SSE endpoint is exempt from the write timeout — the
handler clears the write deadline for that connection so the stream stays open
indefinitely. See [Streaming & SSE](#streaming--sse).
:::

## Middleware stack

Every request passes through the middleware chain in this fixed order. The
outermost layer runs first on the way in and last on the way out:

```
request
  → recovery      (catch panics → 500, never crash the server)
  → logging       (structured request/response log line)
  → CORS          (preflight + response headers)
  → rate limit    (429 when the bucket is exhausted)
  → auth          (401/403 — bypassed for health, metrics, swagger)
  → route handler
```

Ordering rationale:

- **Recovery first** so it wraps everything, including the logger.
- **Logging** before CORS/auth so rejected requests are still recorded.
- **Rate limit before auth** so unauthenticated floods are shed cheaply.
- **Auth last** so only well-formed, non-throttled requests reach principal
  resolution — and so tenant scoping is available to the handler.

:::note Always-public paths
`/healthz`, `/health`, `/health/live`, `/health/ready`, `/metrics`, and
everything under `/swagger*` **bypass the auth middleware** regardless of the
configured auth mode. This keeps liveness/readiness probes and metrics scraping
working without credentials. Because Swagger is reachable anonymously, disable it
on hardened production servers with `CHRONOS_SWAGGER=false` (see
[Authentication](/guides/authentication#security-best-practices)).
:::

:::note Role enforcement
By default the auth middleware only checks that a credential is **valid**, not
what role it carries. Set `CHRONOS_RBAC=true` (or `WithRBAC(true)`) to also gate
`/api/*` routes by role — reads require `viewer`, mutations require `user`. See
[Roles & RBAC](/guides/authentication#roles--rbac).
:::

## Health & readiness

Four health endpoints support container orchestrators and load balancers. All
return JSON `{"status":"ok"}` (HTTP `200`) when healthy and are always
unauthenticated.

| Endpoint | Semantics | Typical use |
|----------|-----------|-------------|
| `GET /healthz` | Process is up | Legacy / general health check |
| `GET /health` | Process is up | Alias of `/healthz` |
| `GET /health/live` | **Liveness** — the process is running | Kubernetes `livenessProbe` |
| `GET /health/ready` | **Readiness** — storage reachable, ready for traffic | Kubernetes `readinessProbe` |

Liveness answers "should this pod be restarted?"; readiness answers "should this
pod receive traffic?". A pod that is alive but not ready (e.g. its database is
briefly unreachable) is pulled from the load-balancer rotation without being
killed.

```yaml
# Kubernetes probe wiring
livenessProbe:
  httpGet: { path: /health/live, port: 8420 }
readinessProbe:
  httpGet: { path: /health/ready, port: 8420 }
```

## Metrics

`GET /metrics` exposes Prometheus-format metrics (request counts, latencies,
in-flight requests, and Go runtime stats). It is unauthenticated so a Prometheus
scraper needs no credentials:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: chronos
    static_configs:
      - targets: ["chronos:8420"]
```

## Streaming & SSE

`GET /api/events/stream` is a long-lived `text/event-stream` connection that
pushes graph and run events as they happen:

- `?session=<id>` scopes the stream to a single session.
- No query parameter subscribes to the **firehose** — every session's events on
  this replica.

```bash
# Follow one session
curl -N http://localhost:8420/api/events/stream?session=sess-123

# Firehose (all sessions)
curl -N http://localhost:8420/api/events/stream
```

Browser clients use the standard `EventSource` API:

```javascript
const es = new EventSource("/api/events/stream?session=sess-123");
es.onmessage = (e) => console.log(JSON.parse(e.data));
```

The write deadline is cleared for this handler so the connection never times out
mid-stream. For the underlying broker and event types, see
[Streaming & SSE](/guides/streaming).

## Multi-tenancy

When auth is enabled, every authenticated principal carries a `TenantID` claim.
The server derives the tenant from the principal and scopes **every storage
operation** to it — so a caller can only ever see sessions, traces, checkpoints,
schedules, and approvals belonging to their own tenant. This makes the API
**IDOR-safe**: passing another tenant's `session_id` returns no data rather than
leaking it.

With auth disabled, all requests run under the single `DefaultTenant`. See
[Multi-Tenancy](/guides/multi-tenancy) for the storage-layer model.

## Graceful shutdown

On `SIGTERM` or `SIGINT` the server:

1. Stops accepting new connections.
2. Lets in-flight requests finish (bounded by a shutdown timeout).
3. Closes the scheduler, approval service, and storage handles.
4. Exits `0`.

This makes rolling deploys safe — Kubernetes sends `SIGTERM`, and the pod drains
before its grace period elapses.

## Running multiple replicas

The control plane is stateless, so horizontal scaling is a matter of pointing
every replica at the **same shared storage** and using store-backed components
where per-request coordination is needed:

| Concern | Single replica | Multiple replicas |
|---------|----------------|-------------------|
| Sessions / checkpoints / traces | Storage backend | Same shared backend |
| Rate limiting | In-memory (default) | `WithRateLimiter(store)` — shared buckets |
| Scheduler | `WithScheduler` | Store-backed scheduler with leasing so a cron job fires **once** across the fleet |
| Approvals | `WithApproval` | Store-backed approval service — any replica can resolve a pending approval |
| SSE firehose | All events | Each replica streams the events it processes; subscribe per session, or fan-in downstream |

:::warning SQLite is single-node
Use [PostgreSQL](/guides/storage) (or another networked backend) for
multi-replica deployments. SQLite is file-local and cannot be shared safely
across pods.
:::

## Production deployment

A complete, opinionated production example — Postgres storage, JWT/JWKS auth, TLS
termination at the ingress, HPA, and probes — lives in
[`deploy/production/`](https://github.com/spawn08/chronos/tree/main/deploy/production)
in the repository. For Kubernetes and Helm specifics see
[Kubernetes & Helm](/deployment/kubernetes); for the full request-per-endpoint
contract see the [REST API Reference](/api/rest-api).

## See also

- [REST API Reference](/api/rest-api) — every endpoint, with curl examples
- [Authentication & Authorization](/guides/authentication) — enabling JWT / API-key auth
- [Streaming & SSE](/guides/streaming) — the event broker and stream protocol
- [Multi-Tenancy](/guides/multi-tenancy) — tenant isolation model
- [Scaling & Best Practices](/guides/scaling-best-practices) — production patterns
