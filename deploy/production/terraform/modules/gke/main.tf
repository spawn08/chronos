# SKELETON — GCP managed stack for Chronos. For production prefer the
# terraform-google-modules/{kubernetes-engine,sql-db} modules. Provider config
# (google >= 5.x), VPC, and private service access are left to the caller.

variable "project_id" { type = string }
variable "region" { type = string }
variable "machine_type" {
  type    = string
  default = "e2-standard-8"
}
variable "db_tier" {
  type    = string
  default = "db-custom-8-32768"
}
variable "redis_size_gb" {
  type    = number
  default = 5
}

resource "google_container_cluster" "this" {
  name                = "chronos-gke"
  project             = var.project_id
  location            = var.region # regional = multi-zone control plane + nodes
  initial_node_count  = 1
  networking_mode     = "VPC_NATIVE"
  datapath_provider   = "ADVANCED_DATAPATH" # Dataplane V2 → NetworkPolicy
  remove_default_node_pool = true
}

resource "google_container_node_pool" "default" {
  name     = "default"
  project  = var.project_id
  location = var.region
  cluster  = google_container_cluster.this.name
  autoscaling {
    min_node_count = 1 # per zone
    max_node_count = 7
  }
  node_config {
    machine_type = var.machine_type
    oauth_scopes = ["https://www.googleapis.com/auth/cloud-platform"]
  }
}

resource "google_sql_database_instance" "postgres" {
  name             = "chronos-pg"
  project          = var.project_id
  region           = var.region
  database_version = "POSTGRES_16"
  settings {
    tier              = var.db_tier
    availability_type = "REGIONAL" # HA across zones
    backup_configuration {
      enabled                        = true
      point_in_time_recovery_enabled = true
    }
  }
  deletion_protection = true
}

resource "google_redis_instance" "redis" {
  name           = "chronos-redis"
  project        = var.project_id
  region         = var.region
  tier           = "STANDARD_HA" # Multi-AZ replica + auto failover
  memory_size_gb = var.redis_size_gb
  redis_version  = "REDIS_7_0"
}

output "kube_context" { value = google_container_cluster.this.name }
output "postgres_connection_name" { value = google_sql_database_instance.postgres.connection_name }
output "redis_host" { value = google_redis_instance.redis.host }
