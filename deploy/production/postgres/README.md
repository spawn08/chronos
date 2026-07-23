# Postgres — CloudNativePG (self-managed) & managed alternatives

ChronosOS uses Postgres as its `storage.Storage` backend
(`CHRONOS_STORAGE_BACKEND=postgres`). This directory ships a self-managed,
HA CloudNativePG (CNPG) cluster that is fully cloud-neutral. If you prefer a
managed database, use the per-cloud equivalents below and simply point
`CHRONOS_STORAGE_DSN` at the managed endpoint (through PgBouncer where noted).

## Self-managed (default) — CloudNativePG

```
cluster.yaml            1 primary + 2 replicas, 3 AZ, WAL archive to object store
pooler.yaml             PgBouncer -rw (writes→primary) and -ro (reads→replicas), transaction mode
scheduled-backup.yaml   daily base backup (RPO ~minutes with continuous WAL)
```

Prereq: install the CNPG operator (see top-level `deploy.sh`/`Makefile`).

Apply order:
```bash
kubectl apply -f cluster.yaml
kubectl wait --for=condition=Ready cluster/chronos-cluster -n chronos --timeout=600s
kubectl apply -f pooler.yaml
kubectl apply -f scheduled-backup.yaml
```

Services created by CNPG:
| Service | Role | Used by |
|---|---|---|
| `chronos-cluster-rw` | primary (read/write) | direct writes (bypass pooler) |
| `chronos-cluster-ro` | replicas (read-only) | direct reads |
| `chronos-pooler-rw` | pooled writes → primary | `CHRONOS_STORAGE_DSN` |
| `chronos-pooler-ro` | pooled reads → replicas | `CHRONOS_STORAGE_DSN_RO` |

Example DSN (matches `k8s/base/deployment.yaml`):
```
postgres://chronos:***@chronos-pooler-rw.chronos.svc:5432/chronos?sslmode=require&pool_max_conns=20
```

Backups: `barmanObjectStore.destinationPath` + `s3Credentials` are the only
cloud-specific fields. See the mapping below.

## Managed alternatives (recommended for teams without DB-on-K8s expertise)

Point `CHRONOS_STORAGE_DSN` at the managed endpoint. Put PgBouncer in front
(managed poolers where available) to keep the §6 connection math. Set
`sslmode=require`.

| Cloud | Service | Pooler | Backup / PITR | Read replicas |
|---|---|---|---|---|
| **AKS / Azure** | Azure Database for PostgreSQL – Flexible Server | PgBouncer built-in (enable) | automated backups, PITR 7–35d, geo-redundant | up to 5 read replicas |
| **EKS / AWS** | Amazon RDS for PostgreSQL or Aurora PostgreSQL | RDS Proxy (transaction pinning aware) | automated backups + PITR, cross-region snapshots | Aurora reader endpoint / RDS replicas |
| **GKE / GCP** | Cloud SQL for PostgreSQL | Cloud SQL connector / PgBouncer sidecar | automated backups, PITR, cross-region | read replicas |

For managed services, skip `cluster.yaml`/`pooler.yaml`/`scheduled-backup.yaml`
and instead provision via the Terraform module for your cloud
(`terraform/modules/{aks,eks,gke}`), then feed the endpoint into the
`chronos/storage` secret consumed by the ExternalSecret.

## Migrations
Run schema migrations as a one-shot pre-deploy Job (`chronos db migrate`-style)
BEFORE rolling the Deployment — never from N racing pods. Use
expand/contract (backward-compatible) migrations. See README §11.
