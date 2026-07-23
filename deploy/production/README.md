# Chronos — Production Reference Deployment (50M+ users)

A **cloud-agnostic**, production-grade Kubernetes reference deployment for the
ChronosOS control plane (`chronos serve`, HTTP `:8420`). It is designed to run
unchanged on **AKS, EKS, or GKE** against any conformant managed Kubernetes
cluster, provisioned and wired together with Terraform (`kubernetes` + `helm`
providers), raw Kustomize manifests, and widely-adopted open-source operators.

> This directory is **self-contained** and additive. It never modifies the
> existing `deploy/helm/chronos` chart — it reuses the same image, port,
> labels, env-var names, and health probes so the two stay interchangeable.

---

## Table of contents

1. [What ChronosOS actually is (design constraints)](#1-what-chronosos-actually-is)
2. [Target: 50M users — capacity model & assumptions](#2-capacity-model)
3. [Architecture diagram](#3-architecture)
4. [Scaling model](#4-scaling-model)
5. [Why scheduler & rate-limiter must be store-backed](#5-store-backed-correctness)
6. [Connection pooling & Postgres math](#6-connection-pooling)
7. [Read-replica routing](#7-read-replicas)
8. [Availability: multi-AZ, spread, PDB](#8-availability)
9. [Autoscaling thresholds (HPA + KEDA)](#9-autoscaling)
10. [Rate limiting & caching](#10-rate-limiting--caching)
11. [Graceful rollout & DB migrations](#11-rollout--migrations)
12. [Backup / DR / RPO / RTO](#12-backup--dr)
13. [Observability: SLOs, golden signals, alerts](#13-observability)
14. [Load testing plan (k6)](#14-load-testing)
15. [Directory layout & deploy order](#15-layout--deploy-order)
16. [Component versions](#16-component-versions)

---

## 1. What ChronosOS actually is

Everything below is grounded in the real server (`os/server.go`, `cli/cmd/root.go`),
**not** invented features:

| Property | Reality in code |
|---|---|
| Binary / entrypoint | `chronos serve :8420` (`deploy/docker/Dockerfile`) |
| Listen port | `8420` (container port `http`) |
| Health | `/healthz`, `/health`, `/health/live`, `/health/ready` (all auth-exempt) |
| Metrics | `/metrics` Prometheus text, auth-exempt |
| REST API | `/api/sessions`, `/api/sessions/state`, `/api/traces`, `/api/schedules`, `/api/schedules/{id}`, `/api/approval/pending`, `/api/approval/respond` |
| Streaming | `/api/events/stream` (SSE) |
| Storage | pluggable `storage.Storage`; **Postgres** for production (`CHRONOS_STORAGE_BACKEND=postgres`, `CHRONOS_STORAGE_DSN`) |
| Scheduler | **store-backed** `StoreScheduler` — claims each due schedule *exactly once* across replicas via an atomic conditional update (`os/scheduler/store_scheduler.go`) |
| Rate limiter | in-process **or** store-backed `SQLLimiter` sharing counters (`os/middleware/ratelimit.go`) |
| Approvals | store-backed approval service (`os/approval`) — visible to any replica |
| Auth | JWT bearer or API-key middleware; health/metrics always exempt |
| Hardening | 15s read / 30s write / 120s idle timeouts, **1 MiB** header & body limits, panic recovery, graceful shutdown on SIGINT/SIGTERM |
| Tracing/metrics export | OTLP/HTTP exporters POST to `<endpoint>/v1/traces` and `<endpoint>/v1/metrics` (`os/trace`, `os/metrics`) |

**The single most important architectural fact:** ChronosOS pods are
**stateless**. All shared state — sessions, memory, audit logs, traces, events,
checkpoints, schedule claims, rate-limit counters, approvals — lives in the
**Storage** backend (Postgres) and, in this reference, a **Redis** layer for
low-latency shared counters/leases. That is what makes horizontal scaling to
50M users a matter of *adding pods*, not re-architecting.

### Environment contract (identical to the Helm chart + this deployment)

```
CHRONOS_STORAGE_BACKEND=postgres
CHRONOS_STORAGE_DSN=postgres://chronos:***@chronos-pooler-rw.chronos:5432/chronos?sslmode=require
CHRONOS_REDIS_URL=redis://:***@chronos-redis-master.chronos:6379/0   # shared rate-limit / scheduler lease / approval fan-out
CHRONOS_AUTH=jwt                                                     # jwt | apikey | off
CHRONOS_JWT_ISSUER=https://issuer.example.com/
CHRONOS_JWT_AUDIENCE=chronos-api
CHRONOS_JWT_JWKS_URL=https://issuer.example.com/.well-known/jwks.json
CHRONOS_API_KEYS=<comma-separated hashed keys>                       # when CHRONOS_AUTH=apikey
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector.observability:4318
```

Secrets (`CHRONOS_STORAGE_DSN`, JWT signing material, `CHRONOS_API_KEYS`) are
delivered via **External Secrets Operator**, never committed.

---

## 2. Capacity model

**Design point:** 50M registered users. We size for *concurrency and request
rate*, not total accounts — the two are decoupled by a realistic activity
factor.

| Assumption | Value | Rationale |
|---|---|---|
| Registered users | 50,000,000 | target |
| Daily active users (DAU) | 20% = 10,000,000 | typical B2C/B2B SaaS DAU/MAU |
| Peak concurrent users | 3% of DAU = 300,000 | diurnal peak factor ~10x over average |
| Requests per active user / min (peak) | 4 | control-plane traffic (list sessions, schedules, SSE keepalive); agent inference is downstream, not on this hot path |
| **Peak API RPS** | 300,000 × 4 / 60 ≈ **20,000 RPS** | headline number |
| SSE long-lived connections at peak | ~150,000 | half of concurrent users hold a stream |
| p99 latency SLO (read) | ≤ 150 ms | golden-signal target |
| p99 latency SLO (write) | ≤ 300 ms | includes DB round-trip |

### Per-pod capacity (measured target, validated by k6)

| Metric | Value | Basis |
|---|---|---|
| CPU request / limit | `500m` / `2` | Go HTTP server, mostly I/O-bound to Postgres/Redis |
| Memory request / limit | `512Mi` / `1Gi` | steady-state + SSE buffers |
| Sustained RPS per pod @ 70% CPU | **~350 RPS** | conservative; Go net/http + pgx pool, p99 < 150ms |
| SSE connections per pod | ~5,000 | one goroutine + small buffer each |

### Replica math

```
Required pods (API)   = ceil(20,000 RPS / 350 RPS-per-pod)   ≈ 58 pods
+ headroom (30% burst)                                        ≈ 75 pods
Required pods (SSE)   = ceil(150,000 conns / 5,000)          ≈ 30 pods (subset overlaps API)
=> HPA:  minReplicas = 12   (survives an AZ loss + normal daily trough)
         maxReplicas = 90   (peak + burst + one-AZ failure margin)
```

- **minReplicas 12** keeps ≥ 4 pods per AZ across 3 AZs so a full AZ loss never
  drops below the trough capacity.
- **maxReplicas 90** covers the 75-pod peak plus a simultaneous AZ failure
  (lose 1/3 → need ~1.5× the surviving capacity).
- Node pool: assume 8 vCPU / 32 GiB nodes → ~14 pods/node (CPU-bound at `500m`
  request) → **~7 nodes** at peak for the app tier, spread 3 AZs, cluster
  autoscaler enabled.

---

## 3. Architecture

```
                                   Internet / 50M clients
                                            │
                                   ┌────────▼─────────┐
                                   │   CDN / Edge     │  (CloudFront / Front Door / Cloud CDN)
                                   │  TLS, WAF, cache │  static + long-poll offload
                                   └────────┬─────────┘
                                            │  HTTPS
                                   ┌────────▼──────────────┐
                                   │  Cloud L4/L7 LB        │  (managed, cloud-neutral Service type=LoadBalancer)
                                   └────────┬──────────────┘
                                            │
                    ┌───────────────────────▼───────────────────────┐
                    │        ingress-nginx  (TLS via cert-manager)   │  HTTP/2, SSE-aware, per-route rate limit
                    └───────────────────────┬───────────────────────┘
                                            │  ClusterIP :8420
        ┌───────────────────────────────────┼───────────────────────────────────┐
        │                    Kubernetes cluster (3 AZ)  namespace: chronos        │
        │                                                                         │
        │   HPA/KEDA-scaled, STATELESS ChronosOS pods (Deployment, 12..90)        │
        │   ┌────────────┐  ┌────────────┐  ┌────────────┐   topologySpread: AZ   │
        │   │ chronos-os │  │ chronos-os │  │ chronos-os │   readOnlyRootFS,      │
        │   │  :8420     │  │  :8420     │  │  :8420     │   non-root, PDB        │
        │   └─────┬──────┘  └─────┬──────┘  └─────┬──────┘                        │
        │         │  writes/reads │  leases/counters                              │
        │  ┌──────▼──────────┐    │        ┌──────▼─────────────┐                 │
        │  │ PgBouncer /     │    └───────►│ Redis (HA, 3 AZ)    │  rate-limit,    │
        │  │ CNPG Pooler     │             │ Sentinel/replicas   │  scheduler      │
        │  │ (transaction    │             └─────────────────────┘  lease,         │
        │  │  mode)          │                                       approval bus  │
        │  └──────┬──────────┘                                                     │
        │         │                                                               │
        │  ┌──────▼───────────────────────────────────────────┐                  │
        │  │  CloudNativePG Cluster (3 AZ)                      │                  │
        │  │   primary ──async──► replica-1 ──► replica-2       │  streaming repl  │
        │  │   -rw service (writes)   -ro service (reads)       │                  │
        │  └──────┬───────────────────────────────────────────┘                  │
        │         │ WAL + base backups                                            │
        └─────────┼───────────────────────────────────────────────────────────────┘
                  │
          ┌───────▼────────┐        ┌──────────────────────────────┐
          │ Object storage │        │ Observability                │
          │ S3/GCS/Azure   │        │  Prometheus + Alertmanager    │
          │ WAL archive +  │        │  Grafana (dashboards)         │
          │ base backups   │        │  OTel Collector (OTLP in/out) │◄── /metrics, OTLP traces
          └────────────────┘        └──────────────────────────────┘
```

---

## 4. Scaling model

**Stateless horizontal scaling.** Each pod is a fungible replica of the same
image with no local durable state (root FS is read-only; only an `emptyDir`
`/tmp` scratch). Any request can hit any pod; any pod can be killed at any time.
This holds because:

- **Sessions / memory / traces / checkpoints** → Postgres (`storage.Storage`).
- **Schedule firing** → shared `StoreScheduler` claim (see §5).
- **Rate-limit counters** → shared Redis / SQL counters (see §5, §10).
- **Approvals** → store-backed, any replica serves `/api/approval/*`.
- **SSE** → clients reconnect to any pod; event bus is Redis-backed for fan-out.

Scaling is therefore linear in pods until the **shared tier** (Postgres
connections, Redis throughput) becomes the bottleneck — which §6 pools around.

**Concurrency per pod.** Go's `net/http` serves each request on its own
goroutine; the practical ceiling is (a) the pgx pool size per pod and (b) CPU.
We cap the per-pod DB pool at **20** connections (see §6) and let CPU (HPA at
70%) be the primary scale signal, with RPS (KEDA) as the fast secondary.

---

## 5. Store-backed correctness (why multi-replica needs a shared store)

With N replicas, three subsystems are **incorrect** if they keep state
in-process. Chronos already ships store-backed implementations for exactly this:

### Scheduler — exactly-once firing
`os/scheduler/store_scheduler.go`: `StoreScheduler` claims each due schedule via
an **atomic conditional update** (`UPDATE ... WHERE next_run <= now AND
claimed_by IS NULL`) in the shared Store. Only one replica wins the row; the
rest see zero rows affected and skip. Result: a cron schedule fires **once**,
not once-per-pod. In this deployment we additionally use a **Redis lease**
(`SET key val NX PX`) as a fast leader gate so the DB is only probed by the
current leader, cutting scheduler DB load by N×. If you run in-process
scheduling with N replicas you get **N duplicate firings** — never do this.

### Rate limiter — shared limits
`os/middleware/ratelimit.go` offers `InProcessLimiter` (per-pod) and
`SQLLimiter` (shared, fixed-window counters). With per-pod limiters a client's
effective limit is `configured × N` — the limit means nothing at scale. We use
a **shared counter** (Redis `INCR`+`EXPIRE`, fixed window) so "100 req/min per
API key" is enforced *globally* regardless of which pod is hit.

### Approvals — cross-replica visibility
Human-in-the-loop approvals (`os/approval`) are store-backed, so an approval
created on pod A is visible on pod B's `/api/approval/pending`. No stickiness
required.

> **Rule:** anything that must be *globally consistent* (counters, leases,
> claims, approvals) goes to the shared store. Postgres is the source of truth;
> Redis is the low-latency accelerator for counters/leases/fan-out.

---

## 6. Connection pooling

Postgres connections are expensive (~10 MB each, backend process per conn). At
90 app pods × a naive 20 conns = **1,800** direct connections — well past a
sane `max_connections`. We interpose **PgBouncer in transaction mode** (via the
CloudNativePG `Pooler`).

```
App tier:        90 pods × 20 pgx conns   = 1,800 client-side connections
                          │ (to PgBouncer, cheap)
PgBouncer:       default_pool_size = 25 per (user,db)
                 transaction mode → conns returned to pool between txns
                          │
Postgres primary: max_connections = 200
                  - 20 reserved (superuser, replication, monitoring, backups)
                  - 180 usable  ≥  PgBouncer server pool (well under 180)
```

- **Transaction mode** multiplexes thousands of idle client connections onto a
  small server pool, because control-plane transactions are short.
- **Writes** → `chronos-pooler-rw` (routes to primary). **Reads** →
  `chronos-pooler-ro` (routes to replicas, see §7).
- `max_connections = 200` on a `db.r6g.2xlarge`-class instance (8 vCPU / 64 GiB)
  is comfortable; raise pooler `default_pool_size` before raising
  `max_connections`.

Per-pod pgx settings (in `CHRONOS_STORAGE_DSN` / ConfigMap):
`pool_max_conns=20 pool_min_conns=2 pool_max_conn_lifetime=30m`.

---

## 7. Read-replicas

CloudNativePG exposes two services:

- `chronos-cluster-rw` / `chronos-pooler-rw` → **primary**, all writes + read-
  your-writes paths (schedule claims, approvals, session creation).
- `chronos-cluster-ro` / `chronos-pooler-ro` → **replicas**, heavy read
  endpoints (`GET /api/sessions`, `GET /api/traces` listings, dashboards).

Routing strategy: the DSN points at the **rw** pooler by default (correctness-
first). To offload reads, set a second env `CHRONOS_STORAGE_DSN_RO` pointing at
the **ro** pooler for list/analytics handlers. Replication is asynchronous, so
never read-after-write from a replica for claim/approval flows — those stay on
`-rw`. Add replicas (`instances: 3 → 5`) to scale read throughput horizontally.

---

## 8. Availability

- **topologySpreadConstraints** — `maxSkew: 1` over
  `topology.kubernetes.io/zone` (`whenUnsatisfiable: DoNotSchedule` in prod
  overlay) forces even spread across 3 AZs. A second constraint over
  `kubernetes.io/hostname` avoids packing a node.
- **PodDisruptionBudget** — `minAvailable: 75%` so a node drain / cluster
  upgrade can never evict more than a quarter of pods at once.
- **Postgres** — CNPG primary + 2 replicas across 3 AZs, automated failover
  (< 30s), synchronous option available for zero-RPO writes.
- **Redis** — HA (replicas + Sentinel or cluster mode) across 3 AZs.
- **Anti-affinity** — soft pod anti-affinity keeps replicas off the same node.

---

## 9. Autoscaling

Two complementary controllers (do **not** point both at the same metric on the
same Deployment in a fight; KEDA *manages* an HPA, so we use **KEDA as the one
scaler** in prod and ship a plain HPA as a portable fallback):

### HPA (fallback / clouds without KEDA) — `k8s/base/hpa.yaml`
- CPU target **70%**, memory target **80%**.
- `behavior`: scale-up fast (100% or +8 pods / 15s, `stabilizationWindow: 0s`),
  scale-down slow (`stabilizationWindow: 300s`, 10%/min) to avoid flapping
  against diurnal noise and SSE connection churn.

### KEDA ScaledObject (prod) — `k8s/overlays/prod/keda-scaledobject.yaml`
- **Primary trigger: RPS** via Prometheus —
  `sum(rate(chronos_http_requests_total[1m]))` with a target of **300 RPS per
  pod** (below the 350 ceiling to keep p99 headroom). RPS leads CPU because a
  burst of cheap requests should still add capacity before latency degrades.
- **Secondary trigger: CPU 70%** (safety net).
- `minReplicaCount: 12`, `maxReplicaCount: 90`, `cooldownPeriod: 300s`,
  `pollingInterval: 15s`.

Rationale: CPU alone lags on I/O-bound Go services (pod can be latency-bound at
40% CPU); RPS is the leading signal, CPU the backstop.

---

## 10. Rate limiting & caching

- **Edge / CDN** absorbs static + repeated GETs; SSE bypasses cache.
- **ingress-nginx** per-IP connection & rate caps as a coarse DoS shield.
- **App-level** shared rate limit (Redis fixed-window, §5) enforces per-API-key
  / per-tenant quotas globally. Default reference: `600 req/min` per key,
  `50 req/s` burst.
- **Body limits**: server enforces **1 MiB** request body & header caps
  (`os/server.go`) — mirrored as `nginx.ingress.kubernetes.io/proxy-body-size:
  1m` so oversized bodies are rejected at the edge, not the pod.
- **Caching**: read-heavy list endpoints can be fronted by short-TTL CDN /
  nginx cache; the source of truth remains Postgres.

---

## 11. Rollout & migrations

- **RollingUpdate**: `maxSurge: 25%`, `maxUnavailable: 0` → never lose capacity
  during a deploy; combined with the 75% PDB, upgrades are zero-downtime.
- **Graceful termination**: `terminationGracePeriodSeconds: 60`; a `preStop`
  `sleep 10` lets the LB/endpoints controller deregister the pod before the
  process gets SIGTERM, and the server drains in-flight requests + SSE within
  `ShutdownTimeout`. Readiness flips to not-ready on SIGTERM.
- **DB migrations**: schema is created by each adapter's `Migrate(ctx)`
  (`storage/adapters/postgres`). Run migrations as a **pre-deploy Job / Helm
  pre-install-hook-equivalent** (a one-shot `chronos db migrate`-style Job)
  **before** rolling the Deployment, never from N racing pods. Migrations must
  be **backward-compatible** (expand/contract): add columns nullable, deploy
  code that writes both, backfill, then contract in a later release. CNPG
  supports major-version upgrades via replica switchover.

---

## 12. Backup / DR

| Objective | Target | Mechanism |
|---|---|---|
| **RPO** | ≤ 5 min (async) / 0 (sync opt-in) | CNPG continuous WAL archiving to object storage |
| **RTO** | ≤ 15 min | CNPG restore from base backup + WAL replay, or promote standby |
| Base backups | daily | CNPG `ScheduledBackup` (`postgres/scheduled-backup.yaml`) |
| WAL archive | continuous | `barmanObjectStore` → S3 / GCS / Azure Blob |
| Retention | 30 days | object-store lifecycle + CNPG retention policy |
| Cross-region DR | warm standby | CNPG replica cluster bootstrapped from the object store in a second region |
| Redis | best-effort | AOF/RDB; Redis holds *ephemeral* counters/leases — rebuildable, not a backup priority |
| Restore drills | quarterly | documented runbook, validated via `barman-cloud` restore into a scratch namespace |

Object storage is the cloud-neutral seam: same CNPG `barmanObjectStore` config,
different `destinationPath` / credentials per cloud.

---

## 13. Observability

**Golden signals** (RED + USE), scraped from `/metrics` via ServiceMonitor and
enriched by OTLP traces through the OTel Collector.

| Signal | Metric | SLO |
|---|---|---|
| Latency | `chronos_http_request_duration_seconds` (histogram) | p99 read ≤ 150ms, write ≤ 300ms |
| Traffic | `rate(chronos_http_requests_total[1m])` | — (capacity signal) |
| Errors | `rate(chronos_http_requests_total{code=~"5.."}[5m])` | < 0.1% error rate |
| Saturation | pod CPU / mem, pgx pool in-use, PgBouncer pool wait | CPU < 70%, pool wait ≈ 0 |
| Availability | readiness / up | 99.95% monthly |

**Alert rules** (`k8s/base/prometheusrule.yaml`):
- `ChronosHighErrorRate` — 5xx > 1% for 5m (page).
- `ChronosHighLatencyP99` — p99 > 300ms for 10m (page).
- `ChronosPodsNotReady` — ready < 75% of desired for 5m (page).
- `ChronosNoLeaderScheduler` / `ChronosScheduleBacklog` — due schedules not
  firing (page).
- `ChronosDBPoolSaturation` — PgBouncer `cl_waiting` > 0 for 5m (warn).
- `ChronosRateLimitStoreDown` — Redis unreachable (warn — limits fail-open/closed
  per config).

Dashboards: `observability/grafana-dashboard.json` (RED overview + saturation +
scheduler + DB pool). Prometheus/Grafana/Alertmanager via
**kube-prometheus-stack**; traces/metrics fan-in via **OpenTelemetry Collector**
(`observability/otel-collector.yaml`) which receives OTLP from Chronos and
exports to Prometheus (metrics) + your tracing backend (Tempo/Jaeger).

---

## 14. Load testing

`loadtest/chronos-load.js` is a **k6** scenario that exercises the real API:
create schedule (`POST /api/schedules`), list sessions (`GET /api/sessions`),
open an SSE stream (`GET /api/events/stream`). It ramps to the target per-pod
RPS and asserts the SLO thresholds (`http_req_duration p99<300ms`,
`http_req_failed<1%`). Run it against a staging replica of prod and scale the VU
count until you either (a) validate ~350 RPS/pod at p99<150ms or (b) find your
real per-pod ceiling — then feed that number back into the §2 replica math and
the KEDA `targetRPS`. See `loadtest/README.md`.

---

## 15. Layout & deploy order

```
deploy/production/
├── README.md                     ← this file
├── Makefile                      ← one-command apply/destroy/verify
├── deploy.sh                     ← ordered installer (namespace→secrets→pg→redis→obs→app)
├── terraform/                    ← cloud-agnostic TF (kubernetes+helm providers)
│   ├── versions.tf variables.tf main.tf outputs.tf terraform.tfvars.example
│   └── modules/{aks,eks,gke}/    ← optional per-cloud cluster+PG+Redis provisioning stubs
├── k8s/
│   ├── base/                     ← Kustomize base (all core objects)
│   └── overlays/prod/            ← prod scale/hardening patches
├── postgres/                     ← CloudNativePG Cluster + Pooler + ScheduledBackup (+ managed alt README)
├── redis/                        ← Bitnami Redis HA values (+ managed alt README)
├── observability/                ← kube-prometheus-stack values, OTel Collector, Grafana dashboard
└── loadtest/                     ← k6 script + README
```

**Install order** (enforced by `deploy.sh` / `make deploy`):

1. **Operators** (cluster-scoped, usually one-time): cert-manager,
   ingress-nginx, external-secrets, KEDA, CloudNativePG, kube-prometheus-stack.
   → `make operators` (Helm) **or** Terraform `helm_release` resources.
2. **namespace** + ResourceQuota/LimitRange + RBAC.
3. **secrets** (ExternalSecret → materialises `chronos-secrets`).
4. **postgres** (CNPG Cluster → wait Ready → Pooler → ScheduledBackup).
5. **redis** (Bitnami HA).
6. **observability** (ServiceMonitor, PrometheusRule, OTel Collector, dashboard).
7. **app** (`kubectl apply -k k8s/overlays/prod`) → rollout status → smoke
   `/health/ready`.

```bash
make operators      # one-time cluster operators (Helm)
make deploy         # ordered install of the whole stack
make verify         # rollout status + /health/ready + a k6 smoke run
make destroy        # reverse-order teardown
# or drive everything through Terraform:
cd terraform && terraform init && terraform apply -var-file=terraform.tfvars
```

---

## 16. Component versions

Pinned, widely-adopted OSS. Bump deliberately; test in staging.

| Component | Chart / image | Version (pinned) | Role |
|---|---|---|---|
| ChronosOS | `ghcr.io/spawn08/chronos` | `0.1.0` | the app |
| ingress-nginx | `ingress-nginx/ingress-nginx` | `4.11.3` | L7 ingress, TLS, SSE |
| cert-manager | `jetstack/cert-manager` | `v1.16.2` | TLS certificates |
| external-secrets | `external-secrets/external-secrets` | `0.10.5` | secret materialisation |
| KEDA | `kedacore/keda` | `2.16.0` | RPS-based autoscaling |
| CloudNativePG | `cnpg/cloudnative-pg` | `0.22.1` (operator 1.24) | Postgres HA + backups + pooler |
| Bitnami Redis | `bitnami/redis` | `20.2.1` | shared counters / leases / fan-out |
| kube-prometheus-stack | `prometheus-community/kube-prometheus-stack` | `65.5.1` | Prometheus/Grafana/Alertmanager |
| OpenTelemetry Collector | `open-telemetry/opentelemetry-collector` | `0.108.0` | OTLP ingest/export |
| Terraform providers | `hashicorp/kubernetes`, `hashicorp/helm`, `gavinbunney/kubectl` | `2.32`, `2.15`, `1.14` | provisioning |

All namespaces, labels (`app.kubernetes.io/name: chronos`, `app: chronos-os`),
the port `8420`, and the env-var names above are **identical** across every file
in this directory and the existing Helm chart.
