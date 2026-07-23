# Module: AKS cluster + managed Postgres + managed Redis (OPTIONAL)

Provisions the **Azure** managed equivalents so the root module can deploy the
Chronos workload onto them. Use this only if you do NOT already have a cluster.

| Chronos need | Azure managed service |
|---|---|
| Kubernetes cluster | **AKS** (`azurerm_kubernetes_cluster`), 3 AZs, autoscaling node pool |
| Postgres (`CHRONOS_STORAGE_DSN`) | **Azure Database for PostgreSQL – Flexible Server** (zone-redundant HA, built-in PgBouncer, PITR backups, read replicas) |
| Redis (`CHRONOS_REDIS_URL`) | **Azure Cache for Redis** Premium (zone-redundant, clustering, persistence) |
| Secrets store | **Azure Key Vault** + External Secrets `AzureKV` ClusterSecretStore (workload identity) |
| Backups object store | **Azure Blob Storage** (CNPG `barmanObjectStore` if self-managing PG) |
| Ingress LB | Azure Standard Load Balancer (via ingress-nginx Service) |

## Usage
```hcl
module "aks" {
  source              = "./modules/aks"
  resource_group_name = "chronos-prod"
  location            = "eastus"
  node_count          = 6
  node_vm_size        = "Standard_D8s_v5"
  postgres_sku        = "GP_Standard_D8s_v3"
  redis_sku           = "Premium"
}
```
Then feed `module.aks.kube_context`, and push the PG/Redis endpoints into Key
Vault under `chronos/storage`, `chronos/redis` for the ExternalSecret to read.

`main.tf` is a **skeleton** — fill in networking (VNet/subnets), workload
identity, and private endpoints per your Azure landing zone.
