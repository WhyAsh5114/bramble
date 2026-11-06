#!/usr/bin/env bash
# Exercise the key-generation CLI with a synthetic generator, never a real key.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
stage="$(mktemp -d "${TMPDIR:-/tmp}/bramble-key-permissions.XXXXXX")"
trap 'rm -rf "$stage"' EXIT

cat > "$stage/brambled" <<'BIN'
#!/usr/bin/env bash
printf 'private: synthetic-private-fixture\npublic:  synthetic-public-fixture\n'
BIN
chmod 700 "$stage/brambled"
printf 'BRAMBLE_PRIVATE_KEY=\nPUBLIC_SETTING=kept\n' > "$stage/node.env"
chmod 644 "$stage/node.env"

output="$(bash "$here/gen-node-key.sh" "$stage/node.env" "$stage/brambled")"
[ "$(stat -c %a "$stage/node.env")" = 600 ] || { echo 'FAIL: generated key file is not mode 600' >&2; exit 1; }
[ "$output" = 'public key (give this to the admin for enroll.ts): synthetic-public-fixture' ] || { echo 'FAIL: unexpected public output' >&2; exit 1; }
grep -q '^BRAMBLE_PRIVATE_KEY=synthetic-private-fixture$' "$stage/node.env"
grep -q '^PUBLIC_SETTING=kept$' "$stage/node.env"
if bash "$here/gen-node-key.sh" "$stage/node.env" "$stage/brambled" > "$stage/retry.out" 2>&1; then
  echo 'FAIL: existing key was overwritten' >&2
  exit 1
fi
printf 'PASS: private file permissions, public-only output, settings preserved, overwrite refused\n'
