---
title: "Kubernetes & Helm"
permalink: /deployment/kubernetes/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

Chronos provides a Helm chart for production Kubernetes deployments with Deployment, Service, Secret, Ingress, HPA, and ServiceAccount templates.

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
    ├── ingress.yaml
    ├── hpa.yaml
    └── serviceaccount.yaml
```

## Configuration

### values.yaml

The chart exposes these values:

```yaml
# Image
image:
  repository: ghcr.io/chronos-ai/chronos
  tag: latest
  pullPolicy: IfNotPresent

# Replicas (overridden by HPA when enabled)
replicaCount: 1

# Service
service:
  type: ClusterIP
  port: 8420

# Secrets (stored as Kubernetes Secret)
secrets:
  storageDSN: ""
  openaiAPIKey: ""
  anthropicAPIKey: ""

# Ingress
ingress:
  enabled: false
  className: nginx
  hosts:
    - host: chronos.example.com
      paths:
        - path: /
          pathType: Prefix
  tls: []

# Autoscaling
autoscaling:
  enabled: false
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
  targetMemoryUtilizationPercentage: 80

# Resources
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: "1"
    memory: 512Mi

# Service Account
serviceAccount:
  create: true
  name: ""
  annotations: {}
```

## Secrets

API keys and database credentials are stored as Kubernetes Secrets:

```bash
helm install chronos deploy/helm/chronos/ \
  --set secrets.storageDSN="postgres://user:pass@db:5432/chronos" \
  --set secrets.openaiAPIKey="sk-..." \
  --set secrets.anthropicAPIKey="sk-ant-..."
```

The Secret is mounted as environment variables in the Deployment:

| Secret Key | Environment Variable |
|-----------|---------------------|
| `storageDSN` | `STORAGE_DSN` |
| `openaiAPIKey` | `OPENAI_API_KEY` |
| `anthropicAPIKey` | `ANTHROPIC_API_KEY` |

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
  --set autoscaling.targetCPUUtilizationPercentage=70
```

## Production Checklist

| Item | Recommendation |
|------|---------------|
| Storage | Use PostgreSQL, not SQLite |
| Secrets | Use external secret manager (Vault, AWS SM) |
| Ingress | Enable TLS termination |
| Autoscaling | Enable HPA with CPU/memory targets |
| Resources | Set requests and limits |
| Health checks | Liveness and readiness probes on `/healthz` |
| Logging | Structured JSON logs to stdout |
| Monitoring | Export metrics via `/metrics` endpoint |

## Upgrading

```bash
helm upgrade chronos deploy/helm/chronos/ \
  --set image.tag=v0.3.0
```

## Uninstalling

```bash
helm uninstall chronos
```
