---
title: "CI/CD"
permalink: /deployment/cicd/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

Chronos includes GitHub Actions workflows for continuous integration and automated releases.

## Continuous Integration

The CI workflow (`.github/workflows/ci.yml`) runs on every push to `main` and on pull requests.

### Pipeline Steps

| Step | Description |
|------|-------------|
| **Lint** | Runs `golangci-lint` with the project config |
| **Build** | Compiles all packages with `go build ./...` |
| **Test** | Runs tests with race detector on Ubuntu and macOS |
| **Vet** | Static analysis with `go vet ./...` |
| **Examples** | Smoke-tests all example programs |
| **Docker** | Verifies the Docker image builds |

### Running Locally

You can replicate the CI pipeline locally using the Makefile:

```bash
make all    # fmt + vet + lint + build
make test   # run tests with race detector
```

Or run individual steps:

```bash
make fmt          # format source files
make vet          # go vet
make lint         # golangci-lint (requires golangci-lint installed)
make build-all    # compile all packages
make test-cover   # tests with HTML coverage report
```

## Release Workflow

The release workflow (`.github/workflows/release.yml`) triggers when a semver tag is pushed.

### Steps

1. **Tests** gate the release
2. **Go module** is published to the Go module proxy
3. **Cross-platform binaries** are built for Linux, macOS, and Windows (amd64 + arm64)
4. **GitHub Release** is created with binaries and SHA-256 checksums
5. **Docker image** is built for `linux/amd64` and `linux/arm64` and pushed to `ghcr.io`

### Cutting a Release

```bash
git tag v0.2.0
git push origin v0.2.0
```

This triggers the full release pipeline. The resulting artifacts:

| Artifact | Location |
|----------|----------|
| Go module | `pkg.go.dev/github.com/spawn08/chronos` |
| CLI binaries | GitHub Release assets |
| Docker image | `ghcr.io/spawn08/chronos:v0.2.0` |
| Checksums | `checksums.txt` in release assets |

### Supported Platforms

| OS | Architecture |
|----|-------------|
| Linux | amd64, arm64 |
| macOS | amd64, arm64 (Apple Silicon) |
| Windows | amd64, arm64 |

## Dependabot

Automated dependency updates are configured in `.github/dependabot.yml`:

| Ecosystem | Schedule |
|-----------|----------|
| Go modules | Weekly |
| GitHub Actions | Weekly |
| Docker | Weekly |

Dependabot creates pull requests for outdated dependencies. The CI workflow validates each PR before merging.

## Branch Protection

Recommended settings for the `main` branch:

- Require status checks to pass (CI lint, build, test)
- Require pull request reviews
- Require linear history
- Do not allow force pushes

## Adding Custom CI Steps

To add a new CI job, edit `.github/workflows/ci.yml`:

```yaml
jobs:
  custom-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.24"
      - run: go build ./...
      - run: your-custom-check
```
