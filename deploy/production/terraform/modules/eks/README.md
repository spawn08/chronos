# Module: EKS cluster + managed Postgres + managed Redis (OPTIONAL)

Provisions the **AWS** managed equivalents. Use only if you don't already have a
cluster.

| Chronos need | AWS managed service |
|---|---|
| Kubernetes cluster | **EKS** (3 AZs, managed node group or Fargate, cluster autoscaler/Karpenter) |
| Postgres (`CHRONOS_STORAGE_DSN`) | **Amazon RDS for PostgreSQL** or **Aurora PostgreSQL** (Multi-AZ, reader endpoint for replicas, automated backups + PITR) |
| Connection pooling | **RDS Proxy** (transaction-aware) in front of RDS/Aurora |
| Redis (`CHRONOS_REDIS_URL`) | **ElastiCache for Redis** (Multi-AZ, automatic failover) or **MemoryDB** for durability |
| Secrets store | **AWS Secrets Manager** + External Secrets `AWS` ClusterSecretStore (IRSA) |
| Backups object store | **S3** (CNPG `barmanObjectStore` if self-managing PG) |
| Ingress LB | NLB/ALB via ingress-nginx or AWS Load Balancer Controller |

## Usage
```hcl
module "eks" {
  source          = "./modules/eks"
  cluster_name    = "chronos-prod"
  region          = "us-east-1"
  instance_types  = ["m6i.2xlarge"]
  db_instance     = "db.r6g.2xlarge"
  redis_node_type = "cache.r6g.large"
}
```
Push the RDS/Aurora + ElastiCache endpoints into Secrets Manager under
`chronos/storage`, `chronos/redis` for the ExternalSecret.

`main.tf` is a **skeleton** — prefer the community `terraform-aws-modules/eks`
and `terraform-aws-modules/rds` modules for production; wire VPC/subnets, IRSA,
and security groups per your AWS landing zone.
