variable "kubeconfig_path" {
  description = "Path to the kubeconfig for the EXISTING managed cluster (AKS/EKS/GKE)."
  type        = string
  default     = "~/.kube/config"
}

variable "kube_context" {
  description = "kubeconfig context to target."
  type        = string
}

variable "namespace" {
  description = "Namespace for the ChronosOS workload."
  type        = string
  default     = "chronos"
}

variable "chronos_image_repository" {
  type    = string
  default = "ghcr.io/spawn08/chronos"
}

variable "chronos_image_tag" {
  type    = string
  default = "0.1.0"
}

variable "chronos_host" {
  description = "Ingress hostname for the control plane."
  type        = string
  default     = "chronos.prod.example.com"
}

# ---- Operator toggles (install cluster operators via Helm). Set false if your
# platform team manages these centrally. ----
variable "install_operators" {
  description = "Install cert-manager, ingress-nginx, external-secrets, KEDA, CNPG, kube-prometheus-stack."
  type        = bool
  default     = true
}

# ---- Pinned chart versions (match README §16). ----
variable "chart_versions" {
  type = map(string)
  default = {
    ingress_nginx         = "4.11.3"
    cert_manager          = "v1.16.2"
    external_secrets      = "0.10.5"
    keda                  = "2.16.0"
    cloudnative_pg        = "0.22.1"
    redis                 = "20.2.1"
    kube_prometheus_stack = "65.5.1"
  }
}

variable "manifests_path" {
  description = "Path to the k8s/ kustomize tree relative to this module."
  type        = string
  default     = "../k8s"
}

variable "enable_keda" {
  description = "Use the KEDA ScaledObject (prod overlay). If false, the plain HPA is used."
  type        = bool
  default     = true
}
