# Observability

Full golden-signal stack for ChronosOS. See README §13 for SLOs and alert
rationale.

## Components
| File | Purpose |
|---|---|
| `kube-prometheus-stack-values.yaml` | Prometheus + Alertmanager + Grafana (Helm values) |
| `otel-collector.yaml` | OpenTelemetry Collector — receives OTLP from Chronos, exports metrics→Prometheus, traces→Tempo/Jaeger |
| `grafana-dashboard.json` | RED + saturation + scheduler + DB-pool dashboard |

The `ServiceMonitor` and `PrometheusRule` for Chronos live with the app in
`k8s/base/` (scraping `/metrics`, alerts on the SLOs).

## Install
```bash
# 1. Operator stack (Prometheus/Grafana/Alertmanager)
helm upgrade --install kube-prometheus-stack \
  prometheus-community/kube-prometheus-stack \
  --namespace observability --create-namespace \
  --version 65.5.1 \
  -f kube-prometheus-stack-values.yaml

# 2. OTel Collector (OTLP ingest → Prometheus/Tempo)
kubectl apply -f otel-collector.yaml

# 3. Import the dashboard (sidecar auto-loads ConfigMaps labelled
#    grafana_dashboard=1), or create the ConfigMap:
kubectl create configmap chronos-dashboard -n observability \
  --from-file=grafana-dashboard.json \
  --dry-run=client -o yaml | \
  kubectl label -f - --local -o yaml grafana_dashboard=1 | kubectl apply -f -
```

## Data flow
```
ChronosOS pods
  ├── /metrics (Prometheus text)  ── scraped by ── Prometheus  (ServiceMonitor)
  └── OTLP/HTTP :4318             ── pushed to ── OTel Collector
                                        ├── metrics → Prometheus remote-write
                                        └── traces  → Tempo/Jaeger
Prometheus ── evaluates ── PrometheusRule (SLO alerts) ── Alertmanager ── pager
Grafana ── queries ── Prometheus ── renders ── chronos-os-red dashboard
```

## Metric names
The dashboard/alerts assume these series (emitted via the Prometheus registry at
`/metrics` and the `hooks.NewPrometheusHook` wiring described in `os/server.go`):
`chronos_http_requests_total{code}`, `chronos_http_request_duration_seconds_bucket`,
`chronos_scheduler_due_backlog`. Adjust the expressions if your build labels
metrics differently.
