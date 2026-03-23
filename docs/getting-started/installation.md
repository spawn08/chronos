# Installation

## Requirements

- **Go 1.24+** — [Download Go](https://go.dev/dl/)
- **C compiler** — Required for SQLite via CGO (gcc on Linux, Xcode Command Line Tools on macOS)

## Install as a Go Module

Add Chronos to your project:

```bash
go get github.com/spawn08/chronos
```

## Build from Source

```bash
git clone https://github.com/spawn08/chronos.git
cd chronos
go build ./...
```

## Verify Installation

Run the quickstart example (no API keys needed):

```bash
go run ./examples/quickstart/
```

Expected output:

```
Result: map[greeting:Hello, World! intent:general_question response:I classified your intent as "general_question". How can I help? user:World]
```

## CLI Binary

Build the CLI for interactive use:

```bash
go build -o bin/chronos ./cli/main.go
./bin/chronos version
```

## Next Steps

- [Quickstart Guide](quickstart.md) — Build your first agent
- [Examples](../guides/examples.md) — Browse all runnable examples
