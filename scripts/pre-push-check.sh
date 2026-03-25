#!/usr/bin/env bash
# pre-push-check.sh — Run the same checks as CI before pushing.
#
# Usage:
#   ./scripts/pre-push-check.sh          # full check (format + vet + lint + build + test)
#   ./scripts/pre-push-check.sh --quick  # fast check (format + vet + build only)
#
# Install as a git pre-push hook:
#   ln -sf ../../scripts/pre-push-check.sh .git/hooks/pre-push
#
# Requirements:
#   - Go 1.24+
#   - golangci-lint (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}✓${NC} $1"; }
fail() { echo -e "${RED}✗${NC} $1"; exit 1; }
warn() { echo -e "${YELLOW}!${NC} $1"; }

QUICK=false
if [[ "${1:-}" == "--quick" ]]; then
    QUICK=true
fi

echo "━━━ Chronos Pre-Push Checks ━━━"
echo ""

# 1. Format check
echo "Checking formatting..."
unformatted=$(gofmt -l . 2>/dev/null || true)
if [[ -n "$unformatted" ]]; then
    fail "Files not formatted:\n$unformatted\nRun: gofmt -w ."
fi
pass "All files formatted"

# 2. Go vet
echo "Running go vet..."
if ! go vet ./... 2>&1; then
    fail "go vet failed"
fi
pass "go vet passed"

# 3. Build
echo "Building all packages..."
if ! go build ./... 2>&1; then
    fail "Build failed"
fi
pass "Build succeeded"

if $QUICK; then
    echo ""
    pass "Quick checks passed"
    exit 0
fi

# 4. golangci-lint (if installed)
LINT_BIN=$(go env GOPATH)/bin/golangci-lint
if [[ -x "$LINT_BIN" ]]; then
    echo "Running golangci-lint..."
    if ! "$LINT_BIN" run --timeout=5m 2>&1; then
        fail "Lint errors found. Fix them before pushing."
    fi
    pass "Lint passed"
else
    warn "golangci-lint not installed — skipping lint check"
    warn "Install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
fi

# 5. Tests
echo "Running tests..."
if ! go test -count=1 -timeout 120s ./... 2>&1; then
    fail "Tests failed"
fi
pass "All tests passed"

echo ""
pass "All pre-push checks passed"
