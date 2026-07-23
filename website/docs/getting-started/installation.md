---
title: "Installation"
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

See [CLI Install](/getting-started/cli-install/) for platform-specific details and manual download options.

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
make install  # installs the `chronos` binary to $GOPATH/bin (on your PATH)
```

Or build a local binary without installing:

```bash
make build       # outputs to bin/chronos
./bin/chronos version
```

Ensure `$GOPATH/bin` (usually `~/go/bin`) is on your `PATH` so the `chronos`
command resolves after `make install`.

## Verify

Run the quickstart example (no API keys needed):

```bash
go run ./examples/quickstart/
```

## Next Steps

- [Quickstart](/getting-started/quickstart/) — Build your first agent
- [Examples](/guides/examples/) — Browse all runnable examples
