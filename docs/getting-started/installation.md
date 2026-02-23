---
title: "Installation"
permalink: /getting-started/installation/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

## Prerequisites

- **Go 1.24+** — Chronos requires Go 1.24 or later. Verify with `go version`.
- **C compiler** — Required for SQLite CGO bindings (`github.com/mattn/go-sqlite3`). On macOS, Xcode Command Line Tools provide this. On Linux, install `build-essential` or equivalent.

## Install the module

Add Chronos to your Go module:

```bash
go get github.com/chronos-ai/chronos
```

The module path is `github.com/chronos-ai/chronos`. Import it in your code:

```go
import "github.com/chronos-ai/chronos/sdk/agent"
```

## Build from source

Clone the repository and build:

```bash
git clone https://github.com/chronos-ai/chronos.git
cd chronos
make build
```

- **`make build`** — Compiles all packages and builds the CLI binary at `bin/chronos`.
- **`make build-all`** — Compiles every package, including examples.

To install the CLI binary to `$GOPATH/bin`:

```bash
make install
```

## Binary installation

Cross-compiled binaries are available for common platforms. Build them with:

```bash
make build-cross
```

This produces binaries in `bin/` for:

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`

Binaries are named `chronos-<os>-<arch>` (e.g., `chronos-darwin-arm64`).

## Verify installation

From the project root:

```bash
go build ./...
go vet ./...
```

Both commands should complete without errors. Run the test suite:

```bash
make test
```
