---
title: "Docker"
permalink: /deployment/docker/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
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

This builds a multi-stage image:
1. **Build stage** -- compiles the CLI binary with Go and CGO (for SQLite)
2. **Runtime stage** -- minimal Debian-slim image with only the binary

## Running

```bash
docker run -p 8420:8420 chronos serve :8420
```

### With Environment Variables

```bash
docker run -p 8420:8420 \
  -e OPENAI_API_KEY=sk-... \
  -e STORAGE_DSN=chronos.db \
  chronos serve :8420
```

### With Persistent Storage

Mount a volume for the SQLite database:

```bash
docker run -p 8420:8420 \
  -v chronos-data:/data \
  -e STORAGE_DSN=/data/chronos.db \
  chronos serve :8420
```

### With PostgreSQL

For production, use PostgreSQL instead of SQLite:

```bash
docker run -p 8420:8420 \
  -e DATABASE_URL="postgres://user:pass@db-host:5432/chronos?sslmode=require" \
  chronos serve :8420
```

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
      - DATABASE_URL=postgres://chronos:chronos@postgres:5432/chronos?sslmode=disable
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
