---
title: "Kubernetes & Helm"
---


Chronos provides a Helm chart for production Kubernetes deployments with Deployment, Service, Secret (or ExternalSecret via the External Secrets Operator), Ingress, HPA, PodDisruptionBudget, ServiceMonitor, and ServiceAccount templates. Pods run hardened by default — non-root user, read-only root filesystem, dropped Linux capabilities, and soft pod anti-affinity across nodes (configurable via `podSecurityContext`, `securityContext`, `podAntiAffinity`, and `topologySpreadConstraints` in `values.yaml`).

## Quick Deploy

```bash
helm install chronos deploy/helm/chronos/ \
  --set image.tag=latest \
  --set secrets.storageDSN="postgres://user:pass@db:5432/chronos"
```

## Chart Structure

```
deploy/helm/chronos/
├── Chart.yaml
├── values.yaml
└── templates/
    ├── deployment.yaml
    ├── secret.yaml
    ├── externalsecret.yaml
    ├── ingress.yaml
    ├── hpa.yaml
    ├── pdb.yaml
    ├── servicemonitor.yaml
    └── serviceaccount.yaml
```

## Configuration

### values.yaml

The chart exposes these values:

```yaml
# Image
image:
  repository: ghcr.io/spawn08/chronos
  tag: "0.1.0"
  pullPolicy: IfNotPresent

# Replicas (overridden by HPA when enabled)
replicaCount: 2

# Storage backend selector, exported as CHRONOS_STORAGE_BACKEND
storage:
  backend: postgres

# Service
service:
  type: ClusterIP
  port: 8420

# Secrets (stored as Kubernetes Secret; create=false + externalSecret.enabled=true
# for production instead of the plaintext dev-only values below)
secrets:
  create: true
  name: chronos-secrets
  storageDSN: "postgres://chronos:changeme@postgres:5432/chronos?sslmode=disable"
  apiKey: ""
  embeddingsKey: ""

# Ingress
ingress:
  enabled: false
  className: ""
  hosts:
    - host: chronos.local
      paths:
        - path: /
          pathType: Prefix
  tls: []

# Autoscaling
autoscaling:
  enabled: false
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilization: 70
  targetMemoryUtilization: 80

# Resources
resources:
  requests:
    cpu: 250m
    memory: 256Mi
  limits:
    cpu: "1"
    memory: 512Mi

# Service Account
serviceAccount:
  create: true
  name: "chronos-os"
  annotations: {}
```

## Secrets

API keys and database credentials are stored as Kubernetes Secrets:

```bash
helm install chronos deploy/helm/chronos/ \
  --set secrets.storageDSN="postgres://user:pass@db:5432/chronos" \
  --set secrets.apiKey="sk-..." \
  --set secrets.embeddingsKey="sk-..."
```

Only `storageDSN` is automatically wired into the Deployment's environment; `apiKey`
and `embeddingsKey` are stored in the Secret for you to reference (e.g. via
`extraEnv` in `values.yaml`) but are not consumed by `chronos serve` itself:

| Secret Key | Environment Variable |
|-----------|---------------------|
| `storageDSN` | `CHRONOS_STORAGE_DSN` (via `secretKeyRef` on key `storage-dsn`) |
| `apiKey` | not auto-wired — reference key `api-key` from `extraEnv` if needed |
| `embeddingsKey` | not auto-wired — reference key `embeddings-key` from `extraEnv` if needed |

## Ingress

Enable external access with an Ingress controller:

```bash
helm install chronos deploy/helm/chronos/ \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=chronos.example.com \
  --set ingress.hosts[0].paths[0].path=/ \
  --set ingress.hosts[0].paths[0].pathType=Prefix
```

With TLS:

```bash
helm install chronos deploy/helm/chronos/ \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=chronos.example.com \
  --set ingress.tls[0].secretName=chronos-tls \
  --set ingress.tls[0].hosts[0]=chronos.example.com
```

## Autoscaling

Enable horizontal pod autoscaling:

```bash
helm install chronos deploy/helm/chronos/ \
  --set autoscaling.enabled=true \
  --set autoscaling.minReplicas=2 \
  --set autoscaling.maxReplicas=10 \
  --set autoscaling.targetCPUUtilization=70
```

## Observability & Availability

Enable a Prometheus Operator `ServiceMonitor` to scrape `/metrics` automatically:

```bash
helm install chronos deploy/helm/chronos/ \
  --set metrics.serviceMonitor.enabled=true \
  --set metrics.serviceMonitor.interval=30s
```

A `PodDisruptionBudget` (enabled by default, `minAvailable: 1`) keeps capacity during voluntary disruptions like node drains and rolling upgrades — tune it under `podDisruptionBudget` in `values.yaml`.

## Production Checklist

| Item | Recommendation |
|------|---------------|
| Storage | Use PostgreSQL, not SQLite |
| Secrets | Use external secret manager (Vault, AWS SM) |
| Ingress | Enable TLS termination |
| Autoscaling | Enable HPA with CPU/memory targets |
| Resources | Set requests and limits |
| Health checks | Liveness and readiness probes on `/health/live` and `/health/ready` |
| Logging | Structured JSON logs to stdout |
| Monitoring | Export metrics via `/metrics` endpoint |

## Production example

A complete, opinionated production manifest set — PostgreSQL storage, JWT/JWKS
authentication, TLS termination at the ingress, HPA, and liveness/readiness
probes wired to `/health/live` and `/health/ready` — lives in
[`deploy/production/`](https://github.com/spawn08/chronos/tree/main/deploy/production)
in the repository. Start from it rather than duplicating the Helm values above.
See [The ChronosOS Server](/guides/server) and
[Authentication & Authorization](/guides/authentication) for what those manifests
configure.

## Upgrading

```bash
helm upgrade chronos deploy/helm/chronos/ \
  --set image.tag=v0.3.0
```

## Uninstalling

```bash
helm uninstall chronos
```
