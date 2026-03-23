#!/usr/bin/env bash
#
# Chronos CLI Installer
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/spawn08/chronos/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/spawn08/chronos/main/install.sh | bash -s -- v0.2.1
#   curl -fsSL https://raw.githubusercontent.com/spawn08/chronos/main/install.sh | bash -s -- --dir /opt/bin
#
# Supported platforms:
#   - Linux   x86_64 / arm64  (Ubuntu, Debian, RHEL, CentOS, Fedora, Alpine)
#   - macOS   x86_64 / arm64  (Intel / Apple Silicon)
#   - Windows x86_64          (via Git Bash / WSL)

set -euo pipefail

REPO="spawn08/chronos"
BINARY_NAME="chronos"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# ── Colors ──────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

info()    { echo -e "${BLUE}[INFO]${NC}  $*"; }
success() { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error()   { echo -e "${RED}[ERROR]${NC} $*" >&2; }

# ── Argument parsing ────────────────────────────────────────
REQUESTED_VERSION=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    v*|V*)
      REQUESTED_VERSION="$1"
      shift
      ;;
    --dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --help|-h)
      echo "Chronos CLI Installer"
      echo ""
      echo "Usage:"
      echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash"
      echo "  curl -fsSL ... | bash -s -- v1.0.0        # specific version"
      echo "  curl -fsSL ... | bash -s -- --dir ~/bin    # custom install dir"
      echo ""
      echo "Environment variables:"
      echo "  INSTALL_DIR    Override install directory (default: /usr/local/bin)"
      exit 0
      ;;
    *)
      error "Unknown argument: $1"
      exit 1
      ;;
  esac
done

# ── Detect platform ─────────────────────────────────────────
detect_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux*)   echo "linux" ;;
    darwin*)  echo "darwin" ;;
    mingw*|msys*|cygwin*) echo "windows" ;;
    *)        error "Unsupported OS: $os"; exit 1 ;;
  esac
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64)       echo "amd64" ;;
    aarch64|arm64)      echo "arm64" ;;
    armv7*|armhf)       echo "arm" ;;
    *)                  error "Unsupported architecture: $arch"; exit 1 ;;
  esac
}

# ── Resolve latest version from GitHub API ───────────────────
get_latest_version() {
  local url="https://api.github.com/repos/${REPO}/releases/latest"
  local version

  if command -v curl &>/dev/null; then
    version=$(curl -fsSL "$url" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name":[[:space:]]*"([^"]+)".*/\1/')
  elif command -v wget &>/dev/null; then
    version=$(wget -qO- "$url" 2>/dev/null | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name":[[:space:]]*"([^"]+)".*/\1/')
  else
    error "Neither curl nor wget found. Please install one of them."
    exit 1
  fi

  if [[ -z "$version" ]]; then
    error "Could not determine latest version. Check https://github.com/${REPO}/releases"
    exit 1
  fi
  echo "$version"
}

# ── Download helper ──────────────────────────────────────────
download() {
  local url="$1"
  local dest="$2"

  if command -v curl &>/dev/null; then
    curl -fSL --progress-bar -o "$dest" "$url"
  elif command -v wget &>/dev/null; then
    wget -q --show-progress -O "$dest" "$url"
  else
    error "Neither curl nor wget found."
    exit 1
  fi
}

# ── Verify checksum ──────────────────────────────────────────
verify_checksum() {
  local file="$1"
  local checksums_file="$2"
  local filename
  filename="$(basename "$file")"

  if ! command -v sha256sum &>/dev/null && ! command -v shasum &>/dev/null; then
    warn "No sha256sum or shasum found — skipping checksum verification"
    return 0
  fi

  local expected
  expected=$(grep "$filename" "$checksums_file" 2>/dev/null | awk '{print $1}')
  if [[ -z "$expected" ]]; then
    warn "Checksum entry not found for $filename — skipping verification"
    return 0
  fi

  local actual
  if command -v sha256sum &>/dev/null; then
    actual=$(sha256sum "$file" | awk '{print $1}')
  else
    actual=$(shasum -a 256 "$file" | awk '{print $1}')
  fi

  if [[ "$actual" != "$expected" ]]; then
    error "Checksum mismatch!"
    error "  Expected: $expected"
    error "  Actual:   $actual"
    exit 1
  fi
  success "Checksum verified"
}

# ── Main ─────────────────────────────────────────────────────
main() {
  echo ""
  echo -e "${CYAN}╔══════════════════════════════════════╗${NC}"
  echo -e "${CYAN}║    Chronos CLI Installer             ║${NC}"
  echo -e "${CYAN}║    Agentic AI Framework for Go       ║${NC}"
  echo -e "${CYAN}╚══════════════════════════════════════╝${NC}"
  echo ""

  local os arch version
  os="$(detect_os)"
  arch="$(detect_arch)"

  if [[ -n "$REQUESTED_VERSION" ]]; then
    version="$REQUESTED_VERSION"
    info "Requested version: $version"
  else
    info "Detecting latest version..."
    version="$(get_latest_version)"
  fi

  info "Version:      ${version}"
  info "Platform:     ${os}/${arch}"
  info "Install dir:  ${INSTALL_DIR}"
  echo ""

  # Determine archive name
  local ext="tar.gz"
  if [[ "$os" == "windows" ]]; then
    ext="zip"
  fi
  local archive_name="chronos-${version}-${os}-${arch}.${ext}"
  local binary_asset="chronos-${os}-${arch}"
  if [[ "$os" == "windows" ]]; then
    binary_asset="${binary_asset}.exe"
  fi

  local base_url="https://github.com/${REPO}/releases/download/${version}"
  local checksums_url="${base_url}/checksums-sha256.txt"

  # Create temp directory
  local tmp_dir
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' EXIT

  # Try archive first, fall back to raw binary
  local use_archive=true
  info "Downloading ${archive_name}..."
  if ! download "${base_url}/${archive_name}" "${tmp_dir}/${archive_name}" 2>/dev/null; then
    use_archive=false
    info "Archive not found, downloading raw binary..."
    download "${base_url}/${binary_asset}" "${tmp_dir}/${binary_asset}"
  fi

  # Download checksums
  info "Downloading checksums..."
  if download "${checksums_url}" "${tmp_dir}/checksums-sha256.txt" 2>/dev/null; then
    if $use_archive; then
      verify_checksum "${tmp_dir}/${archive_name}" "${tmp_dir}/checksums-sha256.txt"
    else
      verify_checksum "${tmp_dir}/${binary_asset}" "${tmp_dir}/checksums-sha256.txt"
    fi
  else
    warn "Checksums file not available — skipping verification"
  fi

  # Extract or move binary
  if $use_archive; then
    info "Extracting..."
    if [[ "$ext" == "tar.gz" ]]; then
      tar xzf "${tmp_dir}/${archive_name}" -C "$tmp_dir"
    else
      unzip -q "${tmp_dir}/${archive_name}" -d "$tmp_dir"
    fi
  fi

  # Find the binary in the temp dir
  local src_binary="${tmp_dir}/${binary_asset}"
  if [[ ! -f "$src_binary" ]]; then
    src_binary="${tmp_dir}/${BINARY_NAME}"
    if [[ "$os" == "windows" ]]; then
      src_binary="${src_binary}.exe"
    fi
  fi

  if [[ ! -f "$src_binary" ]]; then
    error "Binary not found after extraction. Contents of temp dir:"
    ls -la "$tmp_dir"
    exit 1
  fi

  # Install
  local dest="${INSTALL_DIR}/${BINARY_NAME}"
  if [[ "$os" == "windows" ]]; then
    dest="${dest}.exe"
  fi

  info "Installing to ${dest}..."
  mkdir -p "$INSTALL_DIR"

  if [[ -w "$INSTALL_DIR" ]]; then
    cp "$src_binary" "$dest"
    chmod +x "$dest"
  else
    warn "Need elevated privileges to write to ${INSTALL_DIR}"
    sudo cp "$src_binary" "$dest"
    sudo chmod +x "$dest"
  fi

  # Verify installation
  echo ""
  if command -v "$BINARY_NAME" &>/dev/null; then
    success "Chronos installed successfully!"
    echo ""
    "$dest" version 2>/dev/null || true
  else
    success "Chronos installed to ${dest}"
    if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
      echo ""
      warn "${INSTALL_DIR} is not in your PATH. Add it with:"
      echo ""
      echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
      echo ""
      echo "  Or add it to your shell profile (~/.bashrc, ~/.zshrc, etc.)"
    fi
  fi

  echo ""
  echo -e "${GREEN}Get started:${NC}"
  echo "  chronos version          # verify installation"
  echo "  chronos help             # see available commands"
  echo "  chronos repl             # start interactive REPL"
  echo ""
}

main "$@"
