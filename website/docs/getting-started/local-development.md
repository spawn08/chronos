---
title: "Local Development"
sidebar_label: "Local Development"
---


# Local Development

The [`deploy/local/`](https://github.com/spawn08/chronos/tree/main/deploy/local)
directory ships a one-command Docker Compose stack that mirrors the production
platform on your machine: the real `chronos` server running on **Postgres**
(which unlocks the cross-replica scheduler and shared rate limiter), plus a local
observability stack (Prometheus + Grafana).

## Prerequisites

- **Docker** with the **Compose** plugin (`docker compose`, v2).
- Ports `8420`, `5432`, `9090`, and `3000` free on your machine.

## Start the stack

```bash
cp deploy/local/.env.example deploy/local/.env
docker compose -f deploy/local/docker-compose.yml up -d --build
```

There are Makefile shortcuts for the common operations:

```bash
make dev-up      # build + start the stack in the background
make dev-down    # stop the stack (keeps volumes/data)
```

## What comes up

| Service | URL | Notes |
|---------|-----|-------|
| **Chronos server** | http://localhost:8420 | REST API under `/api/*`; Swagger UI at [`/swagger/`](http://localhost:8420/swagger/) |
| **Postgres 16** | `localhost:5432` | Durable store (`chronos`/`chronos`, database `chronos`) |
| **Prometheus** | http://localhost:9090 | Scrapes `chronos:8420/metrics` |
| **Grafana** | http://localhost:3000 | Login `admin`/`admin` — **dev-only**, provisioned datasource + dashboard |

The server exposes health probes at `/health/live` and `/health/ready`; Compose
waits for `/health/ready` before marking the container healthy.

## Storage backends

Chronos selects its storage backend from a single environment variable. The
server uses **exactly one backend at a time**.

| `CHRONOS_STORAGE_BACKEND` | Configured with | Notes |
|---------------------------|-----------------|-------|
| `sqlite` (default) | `CHRONOS_DB_PATH` (file path) | Single-node; good for laptops and CI |
| `postgres` | `CHRONOS_STORAGE_DSN` (connection string) | Networked, multi-replica capable |
| `redis` | `CHRONOS_REDIS_URL` | Full storage backend — **storage only** |

The local stack uses **Postgres by default** (`CHRONOS_STORAGE_BACKEND=postgres`
with `CHRONOS_STORAGE_DSN` pointing at the bundled Postgres container).

### Redis as an alternative backend

Redis is offered as an **alternative full storage backend**, not an add-on. It is
enabled through the Compose `redis` profile and replaces Postgres — the two are
not used together:

```bash
docker compose -f deploy/local/docker-compose.yml --profile redis up -d --build
```

Then set the following in `deploy/local/.env`:

```bash
CHRONOS_STORAGE_BACKEND=redis
CHRONOS_REDIS_URL=redis://redis:6379/0
```

:::warning Redis is a storage backend only
Redis in Chronos is **only** a durable-storage backend. It is **not** used for
scheduling or rate limiting. The store-backed scheduler and shared rate limiter
require Postgres (see below).
:::

## Shared state (scheduler + rate limiter)

`CHRONOS_SHARED_STATE` controls whether the server uses store-backed shared
coordination — a scheduler where each cron job fires **exactly once across all
replicas**, and a **cluster-wide** SQL rate limiter with limits shared across
replicas.

| Backend | Default | Behaviour |
|---------|---------|-----------|
| **Postgres** | on | Store-backed exactly-once scheduler + shared SQL rate limiter, automatically. Set `CHRONOS_SHARED_STATE=false` to opt out. |
| **SQLite** | off | Single-node. Set `CHRONOS_SHARED_STATE=true` to enable it. |
| **Redis** | n/a | Never gets the shared scheduler or shared rate limiter, regardless of this value. |

Because the local stack runs on Postgres, the shared scheduler and rate limiter
are active out of the box — no configuration required.

## Enabling auth locally

Auth is off by default for local development (`CHRONOS_AUTH=none`). To try JWT or
API-key auth, uncomment the relevant block in `deploy/local/.env` — for example:

```bash
CHRONOS_AUTH=apikey
CHRONOS_API_KEYS=local-dev-key:admin:default
```

Restart the server (`make dev-down && make dev-up`) to apply changes. See the
[Authentication & Authorization](/guides/authentication) guide for the full set
of modes, roles, and RBAC.

## Tailing logs

```bash
docker compose -f deploy/local/docker-compose.yml logs -f chronos
```

Swap `chronos` for `postgres`, `prometheus`, or `grafana` to follow a different
service.

## Tearing down

```bash
make dev-clean                                       # stop and remove volumes (wipes data)
# or, directly:
docker compose -f deploy/local/docker-compose.yml down -v
```

`make dev-down` (or `docker compose ... down`) stops the stack but keeps the
Postgres/Grafana volumes; `make dev-clean` / `down -v` also deletes them for a
clean slate.

## Next steps

- [Configuration](/getting-started/configuration/) — full YAML and env reference
- [The ChronosOS Server](/guides/server/) — the control plane in depth
- [Authentication & Authorization](/guides/authentication/) — enabling auth
