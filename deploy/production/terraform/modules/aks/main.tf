# SKELETON — Azure managed stack for Chronos. Fill in networking / identity /
# private endpoints for your landing zone. Provider config is intentionally left
# to the root/caller (azurerm >= 3.100).

variable "resource_group_name" { type = string }
variable "location" { type = string }
variable "node_count" {
  type    = number
  default = 6
}
variable "node_vm_size" {
  type    = string
  default = "Standard_D8s_v5"
}
variable "postgres_sku" {
  type    = string
  default = "GP_Standard_D8s_v3"
}
variable "redis_sku" {
  type    = string
  default = "Premium"
}

resource "azurerm_resource_group" "this" {
  name     = var.resource_group_name
  location = var.location
}

resource "azurerm_kubernetes_cluster" "this" {
  name                = "chronos-aks"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  dns_prefix          = "chronos"

  default_node_pool {
    name                 = "system"
    vm_size              = var.node_vm_size
    node_count           = var.node_count
    zones                = [1, 2, 3]
    auto_scaling_enabled = true
    min_count            = 3
    max_count            = 20
  }

  identity { type = "SystemAssigned" }
  # network_profile { network_plugin = "azure"; network_policy = "cilium" }  # for NetworkPolicy
}

resource "azurerm_postgresql_flexible_server" "this" {
  name                   = "chronos-pg"
  resource_group_name    = azurerm_resource_group.this.name
  location               = azurerm_resource_group.this.location
  version                = "16"
  sku_name               = var.postgres_sku
  storage_mb             = 262144
  high_availability { mode = "ZoneRedundant" }
  # administrator_login / password → store in Key Vault, not here.
  # Enable PgBouncer: azurerm_postgresql_flexible_server_configuration "pgbouncer.enabled" = "true"
}

resource "azurerm_redis_cache" "this" {
  name                = "chronos-redis"
  location            = azurerm_resource_group.this.location
  resource_group_name = azurerm_resource_group.this.name
  capacity            = 1
  family              = "P"
  sku_name            = var.redis_sku
  # zones = [1, 2, 3] for zone redundancy
}

output "kube_context" { value = azurerm_kubernetes_cluster.this.name }
output "postgres_fqdn" { value = azurerm_postgresql_flexible_server.this.fqdn }
output "redis_hostname" { value = azurerm_redis_cache.this.hostname }
