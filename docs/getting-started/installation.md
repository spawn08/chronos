---
title: "Installation"
permalink: /getting-started/installation/
sidebar:
  nav: "docs"
---

# Installation

## Option 1: Install CLI (recommended)

Pre-built binaries for Linux, macOS, and Windows:

```bash
curl -fsSL https://raw.githubusercontent.com/spawn08/chronos/main/install.sh | bash
```

Verify:

```bash
chronos version
```

See [CLI Install]({{ '/getting-started/cli-install/' | relative_url }}) for platform-specific details and manual download options.

## Option 2: Go Module

Add Chronos as a library to your Go project:

```bash
go get github.com/spawn08/chronos
```

**Requirements:** Go 1.24+, C compiler (for SQLite via CGO).

## Option 3: Build from Source

```bash
git clone https://github.com/spawn08/chronos.git
cd chronos
make build    # outputs to bin/chronos
```

Or directly:

```bash
go build -o chronos ./cli/main.go
./chronos version
```

## Verify

Run the quickstart example (no API keys needed):

```bash
go run ./examples/quickstart/
```

## Next Steps

- [Quickstart]({{ '/getting-started/quickstart/' | relative_url }}) — Build your first agent
- [Examples]({{ '/guides/examples/' | relative_url }}) — Browse all runnable examples
