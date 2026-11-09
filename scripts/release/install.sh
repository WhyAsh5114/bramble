#!/bin/sh
# Bramble installer.
#   curl -fsSL https://get-bramble.whyash5114.com/install.sh | sh
#   curl -fsSL https://get-bramble.whyash5114.com/install.sh | sh -s -- --no-open
#
# It downloads one binary from GitHub Releases, checks it against the
# release's SHA256SUMS, installs it to ~/.bramble/bin and starts it. No root.
#
# Flags (or the BRAMBLE_* variable in brackets):
#   --no-start   install only                              [BRAMBLE_NO_START=1]
#   --no-open    start, but do not open a browser          [BRAMBLE_NO_OPEN=1]
#   --version v  install a specific release tag            [BRAMBLE_VERSION]
# BRAMBLE_HOME (default ~/.bramble). BRAMBLE_DOWNLOAD_BASE overrides the
# download URL (https only, or a loopback http URL for tests).
set -eu

REPO="WhyAsh5114/bramble"
HOME_DIR="${BRAMBLE_HOME:-$HOME/.bramble}"
BIN_DIR="$HOME_DIR/bin"
NO_START="${BRAMBLE_NO_START:-0}"
NO_OPEN="${BRAMBLE_NO_OPEN:-0}"
VERSION="${BRAMBLE_VERSION:-}"

say() { printf '%s\n' "$*"; }
die() { printf 'bramble install: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --no-start) NO_START=1 ;;
    --no-open) NO_OPEN=1 ;;
    --version) shift; VERSION="${1:-}" ;;
    --help | -h) say "Usage: curl -fsSL https://get-bramble.whyash5114.com/install.sh | sh -s -- [--no-start] [--no-open] [--version vX.Y.Z]"; exit 0 ;;
    *) die "unknown argument $1 (try --help)" ;;
  esac
  shift
done

if [ -n "$VERSION" ]; then
  BASE="https://github.com/$REPO/releases/download/$VERSION"
else
  BASE="https://github.com/$REPO/releases/latest/download"
fi
BASE="${BRAMBLE_DOWNLOAD_BASE:-$BASE}"
case "$BASE" in
  https://*) ;;
  http://127.0.0.1:* | http://localhost:*) ;;
  *) die "refusing a non-https download URL" ;;
esac

case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *) die "unsupported operating system $(uname -s); Bramble runs on Linux and macOS" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *) die "unsupported CPU $(uname -m); Bramble runs on amd64 and arm64" ;;
esac
TARBALL="bramble_${OS}_${ARCH}.tar.gz"

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v tar >/dev/null 2>&1 || die "tar is required"
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  die "sha256sum or shasum is required to check the download"
fi

say "Bramble for $OS/$ARCH"
say "  download   $BASE/$TARBALL"
say "  install to $BIN_DIR/bramble (no root)"
say "  data in    $HOME_DIR (this device's key; removed by uninstall)"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/bramble-install.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT INT TERM HUP
fetch() { curl -fsSL --retry 2 --connect-timeout 15 --max-time 300 -o "$2" "$1" || die "download failed: $1"; }

fetch "$BASE/SHA256SUMS" "$TMP/SHA256SUMS"
fetch "$BASE/$TARBALL" "$TMP/$TARBALL"
want="$(grep "  $TARBALL\$" "$TMP/SHA256SUMS" | cut -d' ' -f1)"
[ -n "$want" ] || die "SHA256SUMS has no entry for $TARBALL"
got="$(sha256 "$TMP/$TARBALL")"
[ "$want" = "$got" ] || die "checksum mismatch for $TARBALL (want $want, got $got); nothing was installed"
tar -xzf "$TMP/$TARBALL" -C "$TMP"
[ -f "$TMP/bramble" ] || die "the archive has no bramble binary"

mkdir -p "$BIN_DIR"
chmod 700 "$HOME_DIR"
if [ -x "$BIN_DIR/bramble" ]; then
  "$BIN_DIR/bramble" down >/dev/null 2>&1 || true
fi
mv "$TMP/bramble" "$BIN_DIR/bramble"
chmod 755 "$BIN_DIR/bramble"
printf 'bramble-install-v2\n' > "$HOME_DIR/.bramble-install"
say "Installed $("$BIN_DIR/bramble" version)."

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) say "Add it to your PATH:  export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac

if [ "$NO_START" = 1 ]; then
  say "Start it with: $BIN_DIR/bramble up"
  exit 0
fi
if [ "$NO_OPEN" = 1 ]; then
  "$BIN_DIR/bramble" up -d -open=false
else
  "$BIN_DIR/bramble" up -d
fi
say "Open the address above to create a tailnet or join one."
