output "namespace" {
  description = "Namespace the Chronos stack is deployed into."
  value       = kubernetes_namespace.chronos.metadata[0].name
}

output "chronos_host" {
  description = "Ingress hostname for the control plane."
  value       = var.chronos_host
}

output "postgres_rw_service" {
  description = "PgBouncer read/write service (use in CHRONOS_STORAGE_DSN)."
  value       = "chronos-pooler-rw.${var.namespace}.svc:5432"
}

output "postgres_ro_service" {
  description = "PgBouncer read-only service (use in CHRONOS_STORAGE_DSN_RO)."
  value       = "chronos-pooler-ro.${var.namespace}.svc:5432"
}

output "redis_service" {
  description = "Redis master service (use in CHRONOS_REDIS_URL)."
  value       = "chronos-redis-master.${var.namespace}.svc:6379"
}

output "prometheus_service" {
  description = "Prometheus service (KEDA trigger + Grafana datasource)."
  value       = "kube-prometheus-stack-prometheus.observability.svc:9090"
}

output "otel_collector_endpoint" {
  description = "OTLP/HTTP endpoint for OTEL_EXPORTER_OTLP_ENDPOINT."
  value       = "http://otel-collector.observability.svc:4318"
}

output "app_apply_command" {
  description = "Command to apply the application overlay after `terraform apply`."
  value       = var.enable_keda ? "kubectl apply -k ${var.manifests_path}/overlays/prod" : "kubectl apply -k ${var.manifests_path}/base"
}
