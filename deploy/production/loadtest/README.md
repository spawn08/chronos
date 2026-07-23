# Load testing (k6)

`chronos-load.js` drives the real ChronosOS REST API to validate the capacity
model (README §2) and SLOs (README §13).

## Prereqs
- [k6](https://k6.io) installed (`brew install k6`).
- A staging environment that mirrors prod (same DB/Redis sizing).
- A valid JWT (`CHRONOS_AUTH=jwt`) or API key.

## Find the true per-pod ceiling
Scale the app to a single pod so the measured RPS maps to one replica:
```bash
kubectl scale deploy/chronos-os -n chronos --replicas=1
k6 run -e BASE_URL=https://chronos.staging.example.com \
       -e TOKEN="$JWT" \
       -e TARGET_RPS=350 -e DURATION=5m \
       -e SSE_VUS=50 \
       chronos-load.js
```
Increase `TARGET_RPS` until a threshold fails (p99 > 300ms or errors > 1%). The
last passing value is your **per-pod ceiling** — feed it into:
- README §2 replica math, and
- `k8s/overlays/prod/keda-scaledobject.yaml` `threshold` (set it ~15% below the ceiling).

## Full-cluster soak
Against the fully scaled deployment, push toward the §2 headline (20,000 RPS)
and confirm autoscaling reacts (KEDA adds pods before p99 degrades):
```bash
k6 run -e BASE_URL=https://chronos.prod.example.com \
       -e TOKEN="$JWT" -e TARGET_RPS=20000 -e DURATION=30m \
       -e SSE_VUS=2000 chronos-load.js
```

## Interpreting results
| k6 metric | Meaning | Pass |
|---|---|---|
| `http_req_failed` | overall error rate | < 1% |
| `http_req_duration{scenario:api_mix}` p95/p99 | end-to-end latency | p95<150ms, p99<300ms |
| `chronos_list_sessions_ms` p99 | read path (replica) | < 150ms |
| `chronos_create_schedule_ms` p99 | write path (primary) | < 300ms |
| `chronos_sse_connect_ok` | SSE connect success | > 99% |

Cross-check during the run on the Grafana `chronos-os-red` dashboard: RPS,
5xx%, p99, pods ready/desired, CPU vs request, PgBouncer waiting clients. If
`pgbouncer_pools_client_waiting_connections` climbs, the DB pool is the
bottleneck (raise pooler `default_pool_size` / add replicas) — not the app tier.
```
```
