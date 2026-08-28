---
title: "Docker"
---


Chronos ships with a production-ready Dockerfile for containerized deployment.

## Building the Image

```bash
docker build -f deploy/docker/Dockerfile -t chronos .
```

Or use the Makefile:

```bash
make docker-build
```

This builds a multi-stage image (`deploy/docker/Dockerfile`):
1. **Build stage** (`golang:1.25-alpine`) -- compiles the CLI binary with `CGO_ENABLED=0` (the SQLite adapter uses the pure-Go `modernc.org/sqlite` driver, so no C toolchain is needed)
2. **Runtime stage** (`alpine:3.24`) -- minimal image with only the binary, CA certs/timezone data, and a non-root `chronos` user

## Running

```bash
docker run -p 8420:8420 chronos serve :8420
```

### With Environment Variables

```bash
docker run -p 8420:8420 \
  -e OPENAI_API_KEY=sk-... \
  -e CHRONOS_DB_PATH=chronos.db \
  chronos serve :8420
```

### With Persistent Storage

Mount a volume for the SQLite database:

```bash
docker run -p 8420:8420 \
  -v chronos-data:/data \
  -e CHRONOS_DB_PATH=/data/chronos.db \
  chronos serve :8420
```

### With PostgreSQL

For production, use PostgreSQL instead of SQLite:

```bash
docker run -p 8420:8420 \
  -e CHRONOS_STORAGE_BACKEND=postgres \
  -e CHRONOS_STORAGE_DSN="postgres://user:pass@db-host:5432/chronos?sslmode=require" \
  chronos serve :8420
```

See [Configuration](/getting-started/configuration) for the full list of `CHRONOS_*` environment variables (storage backend selection, auth, rate limiting, etc.).

## Docker Compose

Example `docker-compose.yml` for a full stack:

```yaml
version: "3.8"

services:
  chronos:
    build:
      context: .
      dockerfile: deploy/docker/Dockerfile
    ports:
      - "8420:8420"
    environment:
      - OPENAI_API_KEY=${OPENAI_API_KEY}
      - CHRONOS_STORAGE_BACKEND=postgres
      - CHRONOS_STORAGE_DSN=postgres://chronos:chronos@postgres:5432/chronos?sslmode=disable
    depends_on:
      - postgres
    restart: unless-stopped

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: chronos
      POSTGRES_PASSWORD: chronos
      POSTGRES_DB: chronos
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"

  qdrant:
    image: qdrant/qdrant:latest
    ports:
      - "6333:6333"
    volumes:
      - qdrant-data:/qdrant/storage

volumes:
  pgdata:
  qdrant-data:
```

## Local Development Stack

`deploy/local/` ships a one-command **Postgres-backed** local mirror of the
production platform — the real server (unlocking the store-backed
cross-replica scheduler and shared SQL rate limiter) plus Prometheus and
Grafana for observability:

```bash
cd deploy/local
cp .env.example .env
docker compose up -d --build
# …or, from the repo root:
make dev-up
```

| Service | URL | Notes |
|---|---|---|
| Chronos API | http://localhost:8420 | REST + SSE control plane |
| Health (ready) | http://localhost:8420/health/ready | readiness probe |
| Metrics | http://localhost:8420/metrics | Prometheus text format |
| Swagger UI | http://localhost:8420/swagger/ | on by default in `.env.example` |
| Prometheus | http://localhost:9090 | scrapes `chronos:8420/metrics` |
| Grafana | http://localhost:3000 | login `admin` / `admin` — **dev only** |

Grafana ships a provisioned dashboard ("Chronos — Local (Golden Signals)")
covering request rate, error rate, latency percentiles, scheduler backlog, and
Go runtime stats. See [`deploy/local/README.md`](https://github.com/spawn08/chronos/tree/main/deploy/local) for details.

## Cross-Platform Builds

Build for multiple architectures:

```bash
make build-cross
```

This produces binaries for:
- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make docker-build` | Build the Docker image |
| `make docker-push` | Push to container registry |
| `make docker-run` | Build and run locally |
| `make build-cross` | Cross-compile for all platforms |
| `make dev-up` | Build + start the local dev stack (chronos + postgres + prometheus + grafana) |
