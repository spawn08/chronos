# Module: GKE cluster + managed Postgres + managed Redis (OPTIONAL)

Provisions the **GCP** managed equivalents. Use only if you don't already have a
cluster.

| Chronos need | GCP managed service |
|---|---|
| Kubernetes cluster | **GKE** (regional = multi-zone, Dataplane V2 for NetworkPolicy, node auto-provisioning) |
| Postgres (`CHRONOS_STORAGE_DSN`) | **Cloud SQL for PostgreSQL** (HA regional, read replicas, automated backups + PITR) |
| Connection pooling | Cloud SQL Auth Proxy + PgBouncer sidecar (transaction mode) |
| Redis (`CHRONOS_REDIS_URL`) | **Memorystore for Redis** Standard tier (Multi-AZ replica, auto failover) |
| Secrets store | **Secret Manager** + External Secrets `GCPSM` ClusterSecretStore (Workload Identity) |
| Backups object store | **GCS** (CNPG `barmanObjectStore` if self-managing PG) |
| Ingress LB | GCP LB via ingress-nginx or GKE Ingress |

## Usage
```hcl
module "gke" {
  source        = "./modules/gke"
  project_id    = "my-project"
  region        = "us-central1"
  machine_type  = "e2-standard-8"
  db_tier       = "db-custom-8-32768"
  redis_size_gb = 5
}
```
Push the Cloud SQL + Memorystore endpoints into Secret Manager under
`chronos/storage`, `chronos/redis` for the ExternalSecret.

`main.tf` is a **skeleton** — prefer the `terraform-google-modules/kubernetes-engine`
and `sql-db` modules for production; wire VPC/subnets, Workload Identity, and
private service access per your GCP landing zone.
