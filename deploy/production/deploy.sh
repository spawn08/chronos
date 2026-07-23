#!/usr/bin/env bash
# Ordered installer for the Chronos production stack. Cloud-agnostic: targets the
# current kubectl context (an EXISTING managed AKS/EKS/GKE cluster).
#
#   ./deploy.sh operators   # one-time: cluster operators via Helm
#   ./deploy.sh deploy      # ordered: namespace→secrets→postgres→redis→obs→app
#   ./deploy.sh verify      # rollout status + /health/ready smoke
#   ./deploy.sh destroy     # reverse-order teardown
#
# Env overrides: NAMESPACE (chronos), OVERLAY (overlays/prod), IMAGE_TAG (0.1.0).
set -euo pipefail

NAMESPACE="${NAMESPACE:-chronos}"
OVERLAY="${OVERLAY:-overlays/prod}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Pinned chart versions (match README §16).
V_INGRESS=4.11.3
V_CERTMGR=v1.16.2
V_ESO=0.10.5
V_KEDA=2.16.0
V_CNPG=0.22.1
V_REDIS=20.2.1
V_KPS=65.5.1

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }

repos() {
  helm repo add jetstack https://charts.jetstack.io >/dev/null 2>&1 || true
  helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx >/dev/null 2>&1 || true
  helm repo add external-secrets https://charts.external-secrets.io >/dev/null 2>&1 || true
  helm repo add kedacore https://kedacore.github.io/charts >/dev/null 2>&1 || true
  helm repo add cnpg https://cloudnative-pg.github.io/charts >/dev/null 2>&1 || true
  helm repo add bitnami https://charts.bitnami.com/bitnami >/dev/null 2>&1 || true
  helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
  helm repo update >/dev/null
}

operators() {
  repos
  log "cert-manager"
  helm upgrade --install cert-manager jetstack/cert-manager -n cert-manager --create-namespace \
    --version "$V_CERTMGR" --set crds.enabled=true --wait
  log "ingress-nginx"
  helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx -n ingress-nginx --create-namespace \
    --version "$V_INGRESS" --set controller.metrics.enabled=true --wait
  log "external-secrets"
  helm upgrade --install external-secrets external-secrets/external-secrets -n external-secrets --create-namespace \
    --version "$V_ESO" --set installCRDs=true --wait
  log "KEDA"
  helm upgrade --install keda kedacore/keda -n keda --create-namespace --version "$V_KEDA" --wait
  log "CloudNativePG"
  helm upgrade --install cloudnative-pg cnpg/cloudnative-pg -n cnpg-system --create-namespace \
    --version "$V_CNPG" --wait
  log "kube-prometheus-stack"
  helm upgrade --install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
    -n observability --create-namespace --version "$V_KPS" \
    -f "$SCRIPT_DIR/observability/kube-prometheus-stack-values.yaml" --wait
}

deploy() {
  log "1/7 namespace + quotas + rbac (via kustomize base metadata)"
  kubectl apply -f "$SCRIPT_DIR/k8s/base/namespace.yaml"

  log "2/7 secrets (ExternalSecret → chronos-secrets). Ensure the ClusterSecretStore exists."
  kubectl apply -f "$SCRIPT_DIR/k8s/base/externalsecret.yaml" || \
    echo "  (ExternalSecret apply skipped/failed — provision ClusterSecretStore first)"

  log "3/7 postgres (CNPG)"
  kubectl apply -f "$SCRIPT_DIR/postgres/cluster.yaml"
  kubectl wait --for=condition=Ready cluster/chronos-cluster -n "$NAMESPACE" --timeout=600s || true
  kubectl apply -f "$SCRIPT_DIR/postgres/pooler.yaml"
  kubectl apply -f "$SCRIPT_DIR/postgres/scheduled-backup.yaml"

  log "4/7 redis (Bitnami HA)"
  repos
  helm upgrade --install chronos-redis bitnami/redis -n "$NAMESPACE" --create-namespace \
    --version "$V_REDIS" -f "$SCRIPT_DIR/redis/values.yaml" --wait

  log "5/7 observability (OTel Collector; kube-prometheus-stack came from 'operators')"
  kubectl apply -f "$SCRIPT_DIR/observability/otel-collector.yaml"

  log "6/7 application (kustomize $OVERLAY)"
  kubectl apply -k "$SCRIPT_DIR/k8s/$OVERLAY"

  log "7/7 done — run './deploy.sh verify'"
}

verify() {
  log "rollout status"
  kubectl -n "$NAMESPACE" rollout status deploy/chronos-os --timeout=300s
  log "readiness smoke (port-forward :18420 -> /health/ready)"
  kubectl -n "$NAMESPACE" port-forward svc/chronos-os 18420:8420 >/dev/null 2>&1 &
  local pf=$!
  sleep 3
  if curl -fsS http://localhost:18420/health/ready >/dev/null; then
    log "READY ✓"
  else
    log "NOT READY ✗"; kill "$pf" 2>/dev/null || true; exit 1
  fi
  kill "$pf" 2>/dev/null || true
}

destroy() {
  log "reverse-order teardown"
  kubectl delete -k "$SCRIPT_DIR/k8s/$OVERLAY" --ignore-not-found
  kubectl delete -f "$SCRIPT_DIR/observability/otel-collector.yaml" --ignore-not-found
  helm uninstall chronos-redis -n "$NAMESPACE" || true
  kubectl delete -f "$SCRIPT_DIR/postgres/scheduled-backup.yaml" --ignore-not-found
  kubectl delete -f "$SCRIPT_DIR/postgres/pooler.yaml" --ignore-not-found
  kubectl delete -f "$SCRIPT_DIR/postgres/cluster.yaml" --ignore-not-found
  kubectl delete -f "$SCRIPT_DIR/k8s/base/externalsecret.yaml" --ignore-not-found
  log "namespace retained (delete manually if desired): kubectl delete ns $NAMESPACE"
}

case "${1:-}" in
  operators) operators ;;
  deploy)    deploy ;;
  verify)    verify ;;
  destroy)   destroy ;;
  *) echo "usage: $0 {operators|deploy|verify|destroy}"; exit 1 ;;
esac
