# Chronos production stack — one-command lifecycle.
# Cloud-agnostic: acts on the current kubectl context (existing AKS/EKS/GKE).

SHELL := /usr/bin/env bash
NAMESPACE ?= chronos
OVERLAY   ?= overlays/prod
DEPLOY    := ./deploy.sh

.PHONY: help operators deploy verify destroy render loadtest smoke

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

operators: ## One-time: install cluster operators (cert-manager, nginx, ESO, KEDA, CNPG, kube-prometheus-stack)
	NAMESPACE=$(NAMESPACE) $(DEPLOY) operators

deploy: ## Ordered install: namespace -> secrets -> postgres -> redis -> observability -> app
	NAMESPACE=$(NAMESPACE) OVERLAY=$(OVERLAY) $(DEPLOY) deploy

verify: ## Rollout status + /health/ready smoke
	NAMESPACE=$(NAMESPACE) $(DEPLOY) verify

destroy: ## Reverse-order teardown (namespace retained)
	NAMESPACE=$(NAMESPACE) OVERLAY=$(OVERLAY) $(DEPLOY) destroy

render: ## Render the app manifests without applying (review the diff)
	kubectl kustomize k8s/$(OVERLAY)

smoke: ## k6 smoke run (BASE_URL, TOKEN required)
	k6 run -e BASE_URL=$(BASE_URL) -e TOKEN=$(TOKEN) -e TARGET_RPS=50 -e DURATION=1m loadtest/chronos-load.js

loadtest: ## k6 full run (BASE_URL, TOKEN, TARGET_RPS, DURATION)
	k6 run -e BASE_URL=$(BASE_URL) -e TOKEN=$(TOKEN) \
	  -e TARGET_RPS=$(or $(TARGET_RPS),350) -e DURATION=$(or $(DURATION),5m) \
	  loadtest/chronos-load.js
