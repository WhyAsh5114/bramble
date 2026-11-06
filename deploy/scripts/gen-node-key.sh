#!/usr/bin/env bash
# Generate a WireGuard key pair for this node. Writes the private key into the
# env file (BRAMBLE_PRIVATE_KEY=) and prints ONLY the public key.
# Usage: gen-node-key.sh <env-file> [brambled-binary]
set -euo pipefail
file="${1:?usage: $0 <env-file> [brambled-binary]}"
bin="${2:-/opt/bramble/brambled/bin/brambled}"
[ -w "$file" ] || { echo "cannot write $file" >&2; exit 1; }
if grep -Eq '^BRAMBLE_PRIVATE_KEY=.+' "$file"; then
  echo "BRAMBLE_PRIVATE_KEY is already set in $file; refusing to overwrite (rotate with admincli instead)" >&2; exit 1
fi
out="$("$bin" genkey)"
priv="$(printf '%s\n' "$out" | sed -n 's/^private: //p')"
pub="$(printf '%s\n' "$out" | sed -n 's/^public:  //p')"
[ -n "$priv" ] && [ -n "$pub" ] || { echo "unexpected genkey output" >&2; exit 1; }
tmp="$(mktemp "${file}.XXXXXX")"
trap 'rm -f "$tmp"' EXIT
chmod --reference="$file" "$tmp"
K="$priv" awk '/^BRAMBLE_PRIVATE_KEY=/{print "BRAMBLE_PRIVATE_KEY=" ENVIRON["K"]; found=1; next} {print} END{if(!found) print "BRAMBLE_PRIVATE_KEY=" ENVIRON["K"]}' "$file" > "$tmp"
mv "$tmp" "$file"
trap - EXIT
echo "public key (give this to the admin for enroll.ts): $pub"
