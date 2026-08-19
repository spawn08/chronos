---
name: chronos-deploy
description: Deploy Chronos agents to production — Docker Compose, Kubernetes, or embed in an existing Go service. Each target has ready-to-copy files in examples/.
---

# Chronos Deployment

## Activation
Use this skill when:
- Deploying Chronos agents to production
- Creating Docker, Kubernetes, or service-embedding configurations
- Setting up health checks, scaling, or infrastructure

## Target Selection

| Target | Directory | When to use |
|--------|-----------|-------------|
| **Docker** | `examples/docker/` | Single-server, docker-compose deployment |
| **Kubernetes** | `examples/kubernetes/` | Multi-replica, auto-scaling, cloud |
| **Embed in Service** | `examples/embed-service/` | Add agents to an existing Go HTTP server |

Also available: CLI sandbox deploy (`chronos deploy agents.yaml "task"`)

---

## Target: Docker Compose

**Files:** `examples/docker/Dockerfile`, `examples/docker/docker-compose.yaml`, `examples/docker/.env.example`

### Dockerfile
```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o chronos ./cli/main.go

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /app/chronos /usr/local/bin/chronos
COPY agents.yaml /etc/chronos/agents.yaml
EXPOSE 8420
HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
  CMD wget -q --spider http://localhost:8420/health || exit 1
ENTRYPOINT ["chronos"]
CMD ["serve", ":8420"]
```

### docker-compose.yaml
```yaml
services:
  chronos:
    build: .
    ports: ["8420:8420"]
    environment:
      CHRONOS_AUTH: "true"
      CHRONOS_JWT_SECRET: "${JWT_SECRET}"
      CHRONOS_API_KEYS: "${API_KEYS}"
      CHRONOS_CORS_ORIGINS: "${CORS_ORIGINS}"
      CHRONOS_SWAGGER: "false"
      DATABASE_URL: "postgres://chronos:${DB_PASSWORD}@db:5432/chronos?sslmode=disable"
      ANTHROPIC_API_KEY: "${ANTHROPIC_API_KEY}"
    depends_on:
      db: { condition: service_healthy }
    restart: unless-stopped

  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: chronos
      POSTGRES_USER: chronos
      POSTGRES_PASSWORD: "${DB_PASSWORD}"
    volumes: ["pgdata:/var/lib/postgresql/data"]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U chronos"]
      interval: 10s
      timeout: 5s
      retries: 5

  qdrant:  # optional: vector store for RAG/memory
    image: qdrant/qdrant:latest
    ports: ["6333:6333"]
    volumes: ["qdrant_data:/qdrant/storage"]

volumes:
  pgdata:
  qdrant_data:
```

### Deploy
```bash
cp examples/docker/.env.example .env  # edit with real secrets
docker compose up -d
curl http://localhost:8420/health
```

---

## Target: Kubernetes

**Files:** `examples/kubernetes/deployment.yaml`

### deployment.yaml
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: chronos
spec:
  replicas: 2
  selector:
    matchLabels: { app: chronos }
  template:
    metadata:
      labels: { app: chronos }
    spec:
      containers:
        - name: chronos
          image: your-registry/chronos:latest
          ports: [{ containerPort: 8420 }]
          env:
            - { name: CHRONOS_AUTH, value: "true" }
            - { name: CHRONOS_JWT_SECRET, valueFrom: { secretKeyRef: { name: chronos-secrets, key: jwt-secret } } }
            - { name: DATABASE_URL, valueFrom: { secretKeyRef: { name: chronos-secrets, key: database-url } } }
            - { name: ANTHROPIC_API_KEY, valueFrom: { secretKeyRef: { name: chronos-secrets, key: anthropic-api-key } } }
          livenessProbe:
            httpGet: { path: /health, port: 8420 }
            initialDelaySeconds: 15
            periodSeconds: 30
          readinessProbe:
            httpGet: { path: /health, port: 8420 }
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            requests: { cpu: "250m", memory: "256Mi" }
            limits: { cpu: "1000m", memory: "1Gi" }
---
apiVersion: v1
kind: Service
metadata:
  name: chronos
spec:
  selector: { app: chronos }
  ports: [{ port: 80, targetPort: 8420 }]
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: chronos
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls: [{ hosts: ["chronos.example.com"], secretName: chronos-tls }]
  rules:
    - host: chronos.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service: { name: chronos, port: { number: 80 } }
```

### Deploy
```bash
kubectl create secret generic chronos-secrets \
  --from-literal=jwt-secret="your-secret" \
  --from-literal=database-url="postgres://..." \
  --from-literal=anthropic-api-key="sk-ant-..."
kubectl apply -f examples/kubernetes/
kubectl rollout status deployment/chronos
```

---

## Target: Embed in Existing Go Service

**Files:** `examples/embed-service/main.go`

### HTTP Handler
```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "os"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()
    store, _ := sqlite.New("app.db")
    defer store.Close()
    store.Migrate(ctx)

    a, _ := agent.New("api-agent", "API Agent").
        WithModel(model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))).
        WithStorage(store).WithStreaming(true).
        Build()

    // JSON endpoint
    http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
        var req struct{ Message string `json:"message"` }
        json.NewDecoder(r.Body).Decode(&req)
        resp, err := a.Chat(r.Context(), req.Message)
        if err != nil { http.Error(w, err.Error(), 500); return }
        json.NewEncoder(w).Encode(map[string]string{"response": resp.Content})
    })

    // SSE streaming endpoint
    http.HandleFunc("/api/chat/stream", func(w http.ResponseWriter, r *http.Request) {
        var req struct{ Message string `json:"message"` }
        json.NewDecoder(r.Body).Decode(&req)
        flusher, _ := w.(http.Flusher)
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        ch, _ := a.ChatStream(r.Context(), req.Message)
        for resp := range ch {
            data, _ := json.Marshal(map[string]string{"content": resp.Content})
            fmt.Fprintf(w, "data: %s\n\n", data)
            flusher.Flush()
        }
    })

    http.ListenAndServe(":8080", nil)
}
```

### YAML + ChronosOS Hybrid
```go
// Mount ChronosOS alongside your existing app
import chronosOS "github.com/spawn08/chronos/os"

server, _ := chronosOS.NewServer(chronosOS.Config{
    ConfigPath: "agents.yaml",
    Addr:       ":8420",
    Auth:       true,
})
mux := http.NewServeMux()
mux.Handle("/chronos/", http.StripPrefix("/chronos", server.Handler()))
mux.HandleFunc("/api/your-app", yourHandler)
http.ListenAndServe(":8080", mux)
```

---

## CLI Sandbox Deploy (Quick)
```yaml
deployment:
  sandbox:
    backend: "process"       # process | container | k8s
    work_dir: "."
    timeout: "30m"
```
```bash
chronos deploy agents.yaml "Start processing"
```
