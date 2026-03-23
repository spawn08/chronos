---
title: "CLI Install"
permalink: /getting-started/cli-install/
sidebar:
  nav: "docs"
---

# Install Chronos CLI

Pre-built binaries are published to [GitHub Releases](https://github.com/spawn08/chronos/releases) for every tagged version.

## Quick Install (Linux / macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/spawn08/chronos/main/install.sh | bash
```

This auto-detects your OS and architecture, downloads the latest release, verifies the SHA-256 checksum, and installs to `/usr/local/bin`.

### Install a specific version

```bash
curl -fsSL https://raw.githubusercontent.com/spawn08/chronos/main/install.sh | bash -s -- v1.0.0
```

### Install to a custom directory

```bash
curl -fsSL https://raw.githubusercontent.com/spawn08/chronos/main/install.sh | bash -s -- --dir ~/bin
```

Or set the `INSTALL_DIR` environment variable:

```bash
INSTALL_DIR=~/bin curl -fsSL https://raw.githubusercontent.com/spawn08/chronos/main/install.sh | bash
```

## Supported Platforms

| Platform | Architecture | Binary |
|----------|-------------|--------|
| **Linux** | x86_64 (amd64) | `chronos-linux-amd64` |
| **Linux** | ARM64 | `chronos-linux-arm64` |
| **macOS** | Intel (x86_64) | `chronos-darwin-amd64` |
| **macOS** | Apple Silicon (M1/M2/M3/M4) | `chronos-darwin-arm64` |
| **Windows** | x86_64 | `chronos-windows-amd64.exe` |
| **Windows** | ARM64 | `chronos-windows-arm64.exe` |

All binaries are statically linked (`CGO_ENABLED=0`) and require no external dependencies.

## Manual Download

Download the binary for your platform from the [latest release](https://github.com/spawn08/chronos/releases/latest):

### Linux / macOS

```bash
# Download (replace OS and ARCH as needed)
curl -fSL -o chronos.tar.gz \
  https://github.com/spawn08/chronos/releases/latest/download/chronos-v0.1.0-linux-amd64.tar.gz

# Extract
tar xzf chronos.tar.gz

# Install
sudo mv chronos-linux-amd64 /usr/local/bin/chronos
chmod +x /usr/local/bin/chronos
```

### Windows

1. Download the `.zip` from [Releases](https://github.com/spawn08/chronos/releases/latest)
2. Extract `chronos-windows-amd64.exe`
3. Move it to a directory in your `PATH` or add its location to `PATH`

## Verify Installation

```bash
chronos version
```

Expected output:

```
chronos v0.1.0
  commit:    abc1234
  built:     2026-03-23T00:00:00Z
  go:        go1.24.11
  os/arch:   linux/amd64
```

## Verify Checksum

Each release includes a `checksums-sha256.txt` file:

```bash
# Download checksums
curl -fSL -o checksums.txt \
  https://github.com/spawn08/chronos/releases/latest/download/checksums-sha256.txt

# Verify
sha256sum -c checksums.txt
```

## Using as a Go Library

If you want to use Chronos as a Go library rather than a CLI tool:

```bash
go get github.com/spawn08/chronos
```

See the [Quickstart]({{ '/getting-started/quickstart/' | relative_url }}) for usage examples.

## Build from Source

```bash
git clone https://github.com/spawn08/chronos.git
cd chronos
make build    # outputs to bin/chronos
```

Or directly:

```bash
go build -o chronos ./cli/main.go
```

## Updating

Re-run the install script to get the latest version:

```bash
curl -fsSL https://raw.githubusercontent.com/spawn08/chronos/main/install.sh | bash
```

## Uninstall

```bash
sudo rm /usr/local/bin/chronos
```
