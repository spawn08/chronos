# -----------------------------------------------------------------------------
# Chronos production stack — cloud-agnostic deployment onto an EXISTING managed
# Kubernetes cluster (AKS / EKS / GKE). Provisioning the cluster + managed
# Postgres/Redis is delegated to the optional per-cloud modules/ (see their
# READMEs); this root module deploys the workload + operators.
# -----------------------------------------------------------------------------

# ---- Cluster operators (Helm). One-time; toggle with install_operators. ----
resource "helm_release" "cert_manager" {
  count            = var.install_operators ? 1 : 0
  name             = "cert-manager"
  repository       = "https://charts.jetstack.io"
  chart            = "cert-manager"
  version          = var.chart_versions.cert_manager
  namespace        = "cert-manager"
  create_namespace = true
  set {
    name  = "crds.enabled"
    value = "true"
  }
}

resource "helm_release" "ingress_nginx" {
  count            = var.install_operators ? 1 : 0
  name             = "ingress-nginx"
  repository       = "https://kubernetes.github.io/ingress-nginx"
  chart            = "ingress-nginx"
  version          = var.chart_versions.ingress_nginx
  namespace        = "ingress-nginx"
  create_namespace = true
  set {
    name  = "controller.metrics.enabled"
    value = "true"
  }
}

resource "helm_release" "external_secrets" {
  count            = var.install_operators ? 1 : 0
  name             = "external-secrets"
  repository       = "https://charts.external-secrets.io"
  chart            = "external-secrets"
  version          = var.chart_versions.external_secrets
  namespace        = "external-secrets"
  create_namespace = true
  set {
    name  = "installCRDs"
    value = "true"
  }
}

resource "helm_release" "keda" {
  count            = var.install_operators && var.enable_keda ? 1 : 0
  name             = "keda"
  repository       = "https://kedacore.github.io/charts"
  chart            = "keda"
  version          = var.chart_versions.keda
  namespace        = "keda"
  create_namespace = true
}

resource "helm_release" "cloudnative_pg" {
  count            = var.install_operators ? 1 : 0
  name             = "cloudnative-pg"
  repository       = "https://cloudnative-pg.github.io/charts"
  chart            = "cloudnative-pg"
  version          = var.chart_versions.cloudnative_pg
  namespace        = "cnpg-system"
  create_namespace = true
}

resource "helm_release" "kube_prometheus_stack" {
  count            = var.install_operators ? 1 : 0
  name             = "kube-prometheus-stack"
  repository       = "https://prometheus-community.github.io/helm-charts"
  chart            = "kube-prometheus-stack"
  version          = var.chart_versions.kube_prometheus_stack
  namespace        = "observability"
  create_namespace = true
  values           = [file("${path.module}/../observability/kube-prometheus-stack-values.yaml")]
}

# ---- Redis (shared rate-limit / scheduler-lease / approval fan-out). ----
resource "helm_release" "redis" {
  name       = "chronos-redis"
  repository = "https://charts.bitnami.com/bitnami"
  chart      = "redis"
  version    = var.chart_versions.redis
  namespace  = var.namespace
  values     = [file("${path.module}/../redis/values.yaml")]
  depends_on = [kubernetes_namespace.chronos]
}

# ---- Namespace (labels drive PodSecurity + NetworkPolicy selectors). ----
resource "kubernetes_namespace" "chronos" {
  metadata {
    name = var.namespace
    labels = {
      "app.kubernetes.io/name"                 = "chronos"
      "app.kubernetes.io/part-of"              = "chronos"
      "kubernetes.io/metadata.name"            = var.namespace
      "pod-security.kubernetes.io/enforce"     = "restricted"
    }
  }
}

# ---- Postgres (CloudNativePG Cluster + Pooler + ScheduledBackup) via kubectl. ----
data "kubectl_path_documents" "postgres" {
  pattern = "${path.module}/../postgres/{cluster,pooler,scheduled-backup}.yaml"
}

resource "kubectl_manifest" "postgres" {
  for_each   = toset(data.kubectl_path_documents.postgres.documents)
  yaml_body  = each.value
  depends_on = [helm_release.cloudnative_pg, kubernetes_namespace.chronos]
}

# ---- Observability: OTel Collector (kube-prometheus-stack came via Helm). ----
data "kubectl_path_documents" "otel" {
  pattern = "${path.module}/../observability/otel-collector.yaml"
}

resource "kubectl_manifest" "otel" {
  for_each   = toset(data.kubectl_path_documents.otel.documents)
  yaml_body  = each.value
  depends_on = [helm_release.kube_prometheus_stack]
}

# ---- Application: apply the Kustomize prod overlay (KEDA) or base (HPA). ----
# Terraform has no native kustomize build; we render via the kustomization
# data source pattern. Here we shell out through a null-free approach: apply the
# overlay directory using kubectl_manifest per-document after `kustomize build`.
# For simplicity and portability the overlay is applied by deploy.sh/Makefile;
# this resource documents the intended dependency ordering.
#
# If you prefer pure-Terraform, add the `kbst/kustomization` provider and a
# `kustomization_build` data source pointing at:
#   ${var.manifests_path}/overlays/prod   (KEDA)   when var.enable_keda
#   ${var.manifests_path}/base            (HPA)    otherwise
# then iterate its ids into kubernetes_manifest resources.
resource "null_resource" "app_apply_hint" {
  triggers = {
    overlay = var.enable_keda ? "${var.manifests_path}/overlays/prod" : "${var.manifests_path}/base"
    image   = "${var.chronos_image_repository}:${var.chronos_image_tag}"
  }
  depends_on = [
    kubectl_manifest.postgres,
    helm_release.redis,
    kubectl_manifest.otel,
    helm_release.external_secrets,
  ]
}
