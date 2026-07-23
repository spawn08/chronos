# SKELETON — AWS managed stack for Chronos. For production prefer the
# terraform-aws-modules/{eks,rds,vpc} modules. Provider config (aws >= 5.x) and
# VPC/subnets/IRSA are left to the caller/landing zone.

variable "cluster_name" { type = string }
variable "region" { type = string }
variable "instance_types" {
  type    = list(string)
  default = ["m6i.2xlarge"]
}
variable "db_instance" {
  type    = string
  default = "db.r6g.2xlarge"
}
variable "redis_node_type" {
  type    = string
  default = "cache.r6g.large"
}
variable "subnet_ids" {
  type    = list(string)
  default = []
}

resource "aws_eks_cluster" "this" {
  name     = var.cluster_name
  role_arn = "arn:aws:iam::REPLACE:role/eks-cluster-role"
  version  = "1.30"
  vpc_config {
    subnet_ids = var.subnet_ids # 3 AZ private subnets
  }
}

resource "aws_eks_node_group" "default" {
  cluster_name    = aws_eks_cluster.this.name
  node_group_name = "default"
  node_role_arn   = "arn:aws:iam::REPLACE:role/eks-node-role"
  subnet_ids      = var.subnet_ids
  instance_types  = var.instance_types
  scaling_config {
    desired_size = 6
    min_size     = 3
    max_size     = 20
  }
}

resource "aws_db_instance" "postgres" {
  identifier             = "chronos-pg"
  engine                 = "postgres"
  engine_version         = "16"
  instance_class         = var.db_instance
  allocated_storage      = 200
  multi_az               = true
  storage_encrypted      = true
  backup_retention_period = 30
  # username/password → Secrets Manager, not here.
  # Front with aws_db_proxy (RDS Proxy) for transaction pooling.
}

resource "aws_elasticache_replication_group" "redis" {
  replication_group_id       = "chronos-redis"
  description                = "Chronos shared rate-limit/scheduler/approval"
  node_type                  = var.redis_node_type
  num_node_groups            = 1
  replicas_per_node_group    = 2
  automatic_failover_enabled = true
  multi_az_enabled           = true
  engine_version             = "7.1"
  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
}

output "kube_context" { value = aws_eks_cluster.this.arn }
output "postgres_endpoint" { value = aws_db_instance.postgres.endpoint }
output "redis_endpoint" { value = aws_elasticache_replication_group.redis.primary_endpoint_address }
