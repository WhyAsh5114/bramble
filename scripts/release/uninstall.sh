#!/bin/sh
# Bramble uninstaller.
#   curl -fsSL https://get-bramble.whyash5114.com/uninstall.sh | sh
# Stops bramble and deletes ~/.bramble (binary, device key, cache, logs).
# The device stays enrolled in its tailnet until an admin revokes it.
#   --yes        do not ask
#   --keep-data  remove the binary only
set -eu

HOME_DIR="${BRAMBLE_HOME:-$HOME/.bramble}"
ASSUME_YES=0
KEEP_DATA=0
say() { printf '%s\n' "$*"; }
die() { printf 'bramble uninstall: %s\n' "$*" >&2; exit 1; }

for arg in "$@"; do
  case "$arg" in
    --yes | -y) ASSUME_YES=1 ;;
    --keep-data) KEEP_DATA=1 ;;
    --help | -h) say "Usage: curl -fsSL https://get-bramble.whyash5114.com/uninstall.sh | sh -s -- [--yes] [--keep-data]"; exit 0 ;;
    *) die "unknown argument $arg (try --help)" ;;
  esac
done

[ -d "$HOME_DIR" ] || { say "Nothing to remove: $HOME_DIR does not exist."; exit 0; }
HOME_DIR="$(CDPATH= cd -P "$HOME_DIR" && pwd -P)" || die "cannot resolve BRAMBLE_HOME"
USER_HOME="$(CDPATH= cd -P "$HOME" && pwd -P)" || die "cannot resolve HOME"
case "$USER_HOME/" in "$HOME_DIR/"*) die "refusing to remove a home directory or its parent" ;; esac
[ "$HOME_DIR" != / ] || die "refusing to remove /"
[ -f "$HOME_DIR/.bramble-install" ] || [ -f "$HOME_DIR/identity.json" ] ||
  die "$HOME_DIR does not look like a Bramble install; nothing removed"

if [ "$ASSUME_YES" != 1 ]; then
  if [ "$KEEP_DATA" = 1 ]; then
    say "This stops Bramble and removes $HOME_DIR/bin."
  else
    say "This stops Bramble and deletes $HOME_DIR, including this device's key."
  fi
  printf 'Continue? [y/N] '
  read -r answer </dev/tty || answer=
  case "$answer" in y | Y | yes | YES) ;; *) say "Nothing removed."; exit 0 ;; esac
fi

if [ -x "$HOME_DIR/bin/bramble" ]; then
  "$HOME_DIR/bin/bramble" down >/dev/null 2>&1 || true
fi
if [ "$KEEP_DATA" = 1 ]; then
  rm -rf "$HOME_DIR/bin"
  say "Removed $HOME_DIR/bin. The key and state remain in $HOME_DIR."
else
  rm -rf "$HOME_DIR"
  say "Removed $HOME_DIR."
fi
say "The device stays enrolled in its tailnet until an admin revokes it there."
