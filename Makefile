# ──────────────────────────────────────────────────────────────
# Chronos — Makefile
# ──────────────────────────────────────────────────────────────

# Project metadata
MODULE       := github.com/spawn08/chronos
VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT       := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE   := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GO           := go
GOFLAGS      ?=
CGO_ENABLED  ?= 1

# Build flags
LDFLAGS := -s -w \
	-X $(MODULE)/cli/cmd.Version=$(VERSION) \
	-X $(MODULE)/cli/cmd.Commit=$(COMMIT) \
	-X $(MODULE)/cli/cmd.BuildDate=$(BUILD_DATE)

# Output
BIN_DIR   := bin
CLI_BIN   := $(BIN_DIR)/chronos

# Docker
DOCKER_IMAGE := chronos
DOCKER_TAG   ?= $(VERSION)

# ──────────────────────────────────────────────────────────────
# Development
# ──────────────────────────────────────────────────────────────

.PHONY: all
all: fmt vet lint build ## Run fmt, vet, lint, and build

.PHONY: build
build: ## Compile all packages and build the CLI binary
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(CLI_BIN) ./cli/main.go
	@echo "Built $(CLI_BIN) ($(VERSION))"

.PHONY: build-all
build-all: ## Compile every package (including examples)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) ./...

.PHONY: install
install: ## Install the chronos binary to $GOPATH/bin
	CGO_ENABLED=$(CGO_ENABLED) $(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' ./cli/main.go

.PHONY: run
run: build ## Build and run the CLI
	./$(CLI_BIN) $(ARGS)

# ──────────────────────────────────────────────────────────────
# Quality
# ──────────────────────────────────────────────────────────────

.PHONY: fmt
fmt: ## Format all Go source files
	$(GO) fmt ./...

.PHONY: vet
vet: ## Run go vet on all packages
	$(GO) vet ./...

.PHONY: lint
lint: ## Run golangci-lint (install: https://golangci-lint.run/usage/install/)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found — skipping (install: https://golangci-lint.run/usage/install/)"; \
	fi

.PHONY: test
test: ## Run all tests with race detector
	CGO_ENABLED=$(CGO_ENABLED) $(GO) test -race -count=1 -timeout 120s ./...

.PHONY: test-verbose
test-verbose: ## Run all tests with verbose output
	CGO_ENABLED=$(CGO_ENABLED) $(GO) test -race -count=1 -timeout 120s -v ./...

.PHONY: test-cover
test-cover: ## Run tests and generate coverage report
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) test -race -count=1 -timeout 120s -coverprofile=$(BIN_DIR)/coverage.out ./...
	$(GO) tool cover -html=$(BIN_DIR)/coverage.out -o $(BIN_DIR)/coverage.html
	@echo "Coverage report: $(BIN_DIR)/coverage.html"

.PHONY: bench
bench: ## Run benchmarks
	CGO_ENABLED=$(CGO_ENABLED) $(GO) test -bench=. -benchmem -run=^$$ ./...

# ──────────────────────────────────────────────────────────────
# Dependencies
# ──────────────────────────────────────────────────────────────

.PHONY: tidy
tidy: ## Tidy and verify module dependencies
	$(GO) mod tidy
	$(GO) mod verify

.PHONY: deps
deps: ## Download module dependencies
	$(GO) mod download

# ──────────────────────────────────────────────────────────────
# Docker
# ──────────────────────────────────────────────────────────────

.PHONY: docker-build
docker-build: ## Build the Docker image
	docker build -f deploy/docker/Dockerfile \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE):latest .

.PHONY: docker-push
docker-push: ## Push the Docker image to registry
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_IMAGE):latest

.PHONY: docker-run
docker-run: ## Run the Docker container locally
	docker run --rm -p 8420:8420 $(DOCKER_IMAGE):$(DOCKER_TAG)

# ──────────────────────────────────────────────────────────────
# Release
# ──────────────────────────────────────────────────────────────

.PHONY: release-snapshot
release-snapshot: ## Build a release snapshot (requires goreleaser)
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser release --snapshot --clean; \
	else \
		echo "goreleaser not found — building manually"; \
		$(MAKE) build-cross; \
	fi

.PHONY: build-cross
build-cross: ## Cross-compile for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
	@mkdir -p $(BIN_DIR)
	@for pair in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		os=$${pair%%/*}; arch=$${pair##*/}; \
		echo "Building $$os/$$arch..."; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			$(GO) build -ldflags '$(LDFLAGS)' \
			-o $(BIN_DIR)/chronos-$$os-$$arch ./cli/main.go; \
	done
	@echo "Cross-compilation complete"

# ──────────────────────────────────────────────────────────────
# Examples
# ──────────────────────────────────────────────────────────────

.PHONY: example-quickstart
example-quickstart: ## Run the quickstart example
	$(GO) run ./examples/quickstart/main.go

.PHONY: example-multi-agent
example-multi-agent: ## Run the multi-agent example
	$(GO) run ./examples/multi_agent/main.go

.PHONY: example-multi-provider
example-multi-provider: ## Run the multi-provider example
	$(GO) run ./examples/multi_provider/main.go

# ──────────────────────────────────────────────────────────────
# Clean
# ──────────────────────────────────────────────────────────────

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)
	rm -f *.db
	$(GO) clean -cache -testcache

# ──────────────────────────────────────────────────────────────
# Help
# ──────────────────────────────────────────────────────────────

.PHONY: help
help: ## Show this help message
	@echo "Chronos — AI Agent Framework"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
