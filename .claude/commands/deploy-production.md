Deploy Chronos agents to production (Docker, Kubernetes, or cloud).

The deployment target is: $ARGUMENTS

## Instructions

1. Read the deployment config in `sdk/agent/config.go` (DeploymentConfig, SandboxConfig) and the CLI deploy command at `cli/cmd/deploy.go`.

2. Choose the deployment model:

   | Model | Best For | Complexity |
   |-------|----------|------------|
   | **Docker Compose** | Single-server, small teams | Low |
   | **Kubernetes** | Multi-replica, auto-scaling | Medium |
   | **Chronos Sandbox** | CLI-driven, dev/staging | Low |

---

### Option A: Docker Compose Deployment

3a. Create a `Dockerfile`:

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
ENTRYPOINT ["chronos"]
CMD ["serve", ":8420"]
```

4a. Create `docker-compose.yaml`:

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
      OPENAI_API_KEY: "${OPENAI_API_KEY}"
    depends_on:
      db:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8420/health"]
      interval: 30s
      timeout: 10s
      retries: 3
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

  # Optional: vector store for RAG/memory
  qdrant:
    image: qdrant/qdrant:latest
    ports: ["6333:6333"]
    volumes: ["qdrant_data:/qdrant/storage"]

volumes:
  pgdata:
  qdrant_data:
```

5a. Create `.env` for secrets:

```bash
JWT_SECRET=your-32-char-minimum-secret-here
API_KEYS=key-prod-xxxxxxxx
DB_PASSWORD=strong-random-password
CORS_ORIGINS=https://app.example.com
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
```

6a. Deploy:
```bash
docker compose up -d
docker compose logs -f chronos
curl http://localhost:8420/health
```

---

### Option B: Kubernetes Deployment

3b. Create `k8s/deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: chronos
  labels:
    app: chronos
spec:
  replicas: 2
  selector:
    matchLabels:
      app: chronos
  template:
    metadata:
      labels:
        app: chronos
    spec:
      containers:
        - name: chronos
          image: your-registry/chronos:latest
          ports:
            - containerPort: 8420
          env:
            - name: CHRONOS_AUTH
              value: "true"
            - name: CHRONOS_JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: chronos-secrets
                  key: jwt-secret
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: chronos-secrets
                  key: database-url
            - name: ANTHROPIC_API_KEY
              valueFrom:
                secretKeyRef:
                  name: chronos-secrets
                  key: anthropic-api-key
          livenessProbe:
            httpGet:
              path: /health
              port: 8420
            initialDelaySeconds: 15
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /health
              port: 8420
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            requests:
              cpu: "250m"
              memory: "256Mi"
            limits:
              cpu: "1000m"
              memory: "1Gi"
---
apiVersion: v1
kind: Service
metadata:
  name: chronos
spec:
  selector:
    app: chronos
  ports:
    - port: 80
      targetPort: 8420
  type: ClusterIP
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: chronos
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
    - hosts: ["chronos.example.com"]
      secretName: chronos-tls
  rules:
    - host: chronos.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: chronos
                port:
                  number: 80
```

4b. Create K8s secrets:
```bash
kubectl create secret generic chronos-secrets \
  --from-literal=jwt-secret="your-secret" \
  --from-literal=database-url="postgres://..." \
  --from-literal=anthropic-api-key="sk-ant-..."
```

5b. Deploy:
```bash
kubectl apply -f k8s/
kubectl rollout status deployment/chronos
kubectl logs -f deployment/chronos
```

---

### Option C: Chronos Sandbox Deploy (CLI)

3c. Use the YAML deployment block:

```yaml
agents:
  - id: "my-agent"
    name: "Production Agent"
    model:
      provider: anthropic
      model: claude-sonnet-4-20250514
      api_key: ${ANTHROPIC_API_KEY}

deployment:
  name: "my-agent-deploy"
  sandbox:
    backend: "container"     # process | container | k8s
    image: "chronos-agent:latest"
    work_dir: "/app"
    network: "bridge"
    timeout: "30m"
```

4c. Deploy via CLI:
```bash
go run ./cli/main.go deploy agents.yaml "Start processing"
```

---

### Production Checklist

```
[ ] PostgreSQL for storage (not SQLite)
[ ] Auth enabled (CHRONOS_AUTH=true)
[ ] JWT secret is random, 32+ characters
[ ] Swagger disabled (CHRONOS_SWAGGER=false)
[ ] CORS restricted to known origins
[ ] Health checks configured
[ ] Resource limits set (CPU/memory)
[ ] API keys stored in secrets manager, not env files
[ ] TLS termination via reverse proxy or ingress
[ ] Log aggregation configured
[ ] Monitoring/alerting on /health endpoint
[ ] Database backups scheduled
[ ] Agent configs version-controlled
```
