#!/usr/bin/env bash
# Builds the release: one static `bramble` binary per target, with the local
# web UI embedded, as release/bramble_<os>_<arch>.tar.gz plus release/SHA256SUMS.
# Needs Go, Node.js and pnpm-installed dependencies.
#
# Usage: scripts/release.sh [os/arch ...]
#   targets default to linux/amd64 linux/arm64 darwin/arm64 darwin/amd64
#   VERSION            baked into `bramble version` (default: git describe)
#   BRAMBLE_SKIP_UI=1  embed whatever is already in node-daemon/ui/dist
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

targets=("$@")
if [ "${#targets[@]}" -eq 0 ]; then
  targets=(linux/amd64 linux/arm64 darwin/arm64 darwin/amd64)
fi
version="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo unknown)}"
out="$root/release"
stage="$out/.stage"

if [ "${BRAMBLE_SKIP_UI:-0}" != 1 ]; then
  echo "== local UI"
  pnpm --filter @bramble/local-ui build
  pnpm --filter @bramble/local-ui embed
fi

rm -rf "$out"
mkdir -p "$stage"
for target in "${targets[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  echo "== bramble $version for $os/$arch"
  dir="$stage/bramble_${os}_${arch}"
  mkdir -p "$dir"
  (cd node-daemon && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X main.version=$version" -o "$dir/bramble" .)
  tar -C "$dir" -czf "$out/bramble_${os}_${arch}.tar.gz" bramble
done
rm -rf "$stage"
(cd "$out" && if command -v sha256sum >/dev/null; then sha256sum ./*.tar.gz; else shasum -a 256 ./*.tar.gz; fi | sed 's#\./##' > SHA256SUMS)
cp scripts/release/install.sh scripts/release/uninstall.sh "$out/"
echo "== done: $out"
ls -la "$out"
