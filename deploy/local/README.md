# Chronos — Local Development Stack

A one-command local mirror of the Chronos production platform. It runs the
**real** server on **Postgres** — which unlocks the store-backed cross-replica
scheduler (exactly-once cron) and the shared SQL rate limiter — alongside a
local observability stack (Prometheus + Grafana).

This is the same platform as `deploy/production`, just single-node and wired for
local dev.

## Quickstart

```bash
cd deploy/local
cp .env.example .env
docker compose up -d --build
# …or, from the repo root:
make dev-up
```

First boot builds the `chronos` image from the repo root using
`deploy/docker/Dockerfile`, starts Postgres, waits for it to be healthy, then
starts the server, Prometheus, and Grafana.

## URLs

| Service | URL | Notes |
|---|---|---|
| Chronos API | http://localhost:8420 | REST + SSE control plane |
| Health (live) | http://localhost:8420/health/live | liveness |
| Health (ready) | http://localhost:8420/health/ready | readiness (used by healthcheck) |
| Metrics | http://localhost:8420/metrics | Prometheus text format |
| Swagger UI | http://localhost:8420/swagger/ | set `CHRONOS_SWAGGER=true` (on by default in `.env.example`) |
| Prometheus | http://localhost:9090 | scrapes `chronos:8420/metrics` |
| Grafana | http://localhost:3000 | login `admin` / `admin` — **dev only** |

Grafana ships a provisioned Prometheus datasource and the
**"Chronos — Local (Golden Signals)"** dashboard (request rate, 5xx error rate,
latency p50/p95/p99, scheduler backlog, and Go runtime).

## Configuration

All knobs live in `.env` (copied from `.env.example`). Defaults make **Postgres**
the primary backend:

```
CHRONOS_STORAGE_BACKEND=postgres
CHRONOS_STORAGE_DSN=postgres://chronos:chronos@postgres:5432/chronos?sslmode=disable
```

With Postgres, `CHRONOS_SHARED_STATE` is **on by default**, so the store-backed
scheduler and the shared rate limiter are active.

### Enable auth

Auth is opt-in. Edit `.env` and uncomment one of the examples:

```dotenv
# JWT
CHRONOS_AUTH=jwt
CHRONOS_JWT_SECRET=change-me-local-dev-secret
CHRONOS_JWT_ISSUER=chronos-local
CHRONOS_JWT_AUDIENCE=chronos

# …or API keys ("key:role:tenant,...")
CHRONOS_AUTH=apikey
CHRONOS_API_KEYS=local-dev-key:admin:default
```

Then `make dev-restart`.

### Switch to the Redis backend

**Redis is an ALTERNATIVE storage backend only.** It is a full storage backend
but does **not** provide the shared scheduler or shared rate limiter (those
require Postgres). It is not used alongside Postgres, so it lives behind a
compose profile.

```bash
# 1. Edit .env:
#    CHRONOS_STORAGE_BACKEND=redis
#    CHRONOS_REDIS_URL=redis://redis:6379/0
# 2. Bring the stack up with the redis profile:
docker compose --profile redis up -d --build
```

Postgres can be left running or omitted; the server only talks to Redis when
`CHRONOS_STORAGE_BACKEND=redis`.

## Day-to-day

```bash
make dev-up        # build + start (detached)
make dev-ps        # list stack containers
make dev-logs      # tail all logs (Ctrl-C to stop)
make dev-restart   # restart after editing .env
make dev-down      # stop and remove containers (keeps volumes/data)
make dev-clean     # stop and remove containers + named volumes (wipe data)
```

Or drive compose directly:

```bash
docker compose -f deploy/local/docker-compose.yml logs -f chronos
docker compose -f deploy/local/docker-compose.yml down
```

## Notes

- **Single-node** by design. This stack mirrors `deploy/production` but runs one
  replica; the cross-replica scheduler and shared limiter still exercise the same
  Postgres-backed code paths.
- **Credentials are dev-only.** Postgres `chronos/chronos` and Grafana
  `admin/admin` are insecure defaults — never reuse them outside local dev.
- **Healthcheck** probes `/health/ready` with BusyBox `wget` (present in the
  alpine runtime image). There is no `chronos health` subcommand.
- **Graceful shutdown**: the server drains on `SIGTERM` (30s grace period).
