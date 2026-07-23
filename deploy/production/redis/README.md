# Redis — shared state layer (self-managed & managed alternatives)

## What Chronos uses Redis for
- **Rate-limit counters** — global fixed-window counters (`INCR`+`EXPIRE`) so a
  per-key/per-tenant quota is enforced across all replicas, not per-pod
  (README §5/§10).
- **Scheduler leader lease** — `SET key val NX PX <ttl>` gate so only the leader
  probes the DB for due schedules, complementing the store-backed exactly-once
  claim in `os/scheduler` (README §5).
- **Approval / event fan-out** — pub/sub so an event/approval on one pod reaches
  SSE clients connected to any pod.

Consumed via `CHRONOS_REDIS_URL` (from the `chronos-secrets` Secret). Redis is a
low-latency accelerator; Postgres is the durable source of truth.

## Self-managed (default) — Bitnami Redis
```bash
helm upgrade --install chronos-redis bitnami/redis \
  --namespace chronos \
  --version 20.2.1 \
  -f values.yaml
```
`values.yaml` provisions replication + Sentinel across 3 AZs with AOF, auth from
an existing Secret, and a ServiceMonitor. Endpoint (Sentinel-fronted master):
`chronos-redis-master.chronos.svc:6379`.

## Managed alternatives
Point `CHRONOS_REDIS_URL` at the managed endpoint (`rediss://` for TLS).

| Cloud | Service | HA | Notes |
|---|---|---|---|
| **AKS / Azure** | Azure Cache for Redis (Premium) / Azure Managed Redis | zone-redundant, replicas | supports clustering, persistence |
| **EKS / AWS** | Amazon ElastiCache for Redis / MemoryDB | Multi-AZ with automatic failover | MemoryDB adds durability |
| **GKE / GCP** | Memorystore for Redis (Standard tier) | Multi-AZ replica + auto failover | read replicas optional |

Provision via the Terraform module for your cloud
(`terraform/modules/{aks,eks,gke}`) and feed the endpoint into the
`chronos/redis` secret backing the ExternalSecret.
