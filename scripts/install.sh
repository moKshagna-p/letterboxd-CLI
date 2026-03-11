#!/bin/sh

set -eu

REPO="moKshagna-p/letterboxd-TUI-Heatmap"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

need_cmd curl
need_cmd tar
need_cmd mktemp
need_cmd uname

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$os" in
  linux) ;;
  *)
    echo "unsupported OS for this installer: $os" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

archive="letterboxd-tui_linux_${arch}.tar.gz"
url="https://github.com/$REPO/releases/latest/download/$archive"

curl -fsSL "$url" -o "$tmpdir/$archive"
tar -xzf "$tmpdir/$archive" -C "$tmpdir"

install -d "$INSTALL_DIR"
install "$tmpdir/letterboxd-tui" "$INSTALL_DIR/letterboxd-tui"

echo "installed letterboxd-tui to $INSTALL_DIR/letterboxd-tui"
