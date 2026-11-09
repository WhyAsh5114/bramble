#!/usr/bin/env bash
# Check that every required variable is present and non-empty in an env file.
# Prints variable NAMES only. Never prints, sources or echoes a value.
# Usage: check-env.sh gateway|worker|relay|admin [env-file]
set -euo pipefail

role="${1:-}"
node_req="BRAMBLE_NODE_LABEL BRAMBLE_TAILNET_NAME BRAMBLE_TAILNET_REGISTRY BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK BRAMBLE_RELAY_REGISTRY BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK BRAMBLE_PRIVATE_KEY SEPOLIA_RPC_URL HEDERA_CLIENT_ACCOUNT_ID HEDERA_CLIENT_PRIVATE_KEY BRAMBLE_SERVICE_NAME"
case "$role" in
  gateway) file="${2:-/etc/bramble/gateway.env}"; req="$node_req";;
  worker) file="${2:-/etc/bramble/worker.env}"; req="$node_req BRAMBLE_GATEWAY_LABEL";;
  relay) file="${2:-/etc/bramble/relay-relay1.env}"; req="RELAY_PAYEE_ACCOUNT_ID";;
  admin) file="${2:-/etc/bramble/admin.env}"
    req="BRAMBLE_TAILNET_NAME BRAMBLE_TAILNET_REGISTRY BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK BRAMBLE_RELAY_REGISTRY BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK SEPOLIA_RPC_URL SEPOLIA_PRIVATE_KEY APPROVAL_DEMO_TOKEN";;
  *) echo "usage: $0 gateway|worker|relay|admin [env-file]" >&2; exit 2;;
esac

[ -r "$file" ] || { echo "cannot read $file" >&2; exit 1; }
mode="$(stat -c %a "$file" 2>/dev/null || stat -f %Lp "$file")"
bad=0
case "$mode" in 600|400) ;; *) echo "FAIL  $file mode is $mode, expected 600"; bad=1;; esac
for name in $req; do
  if grep -Eq "^${name}=.+" "$file"; then echo "ok    $name"; else echo "FAIL  $name is missing or empty"; bad=1; fi
done
# An optional name set to an empty value overrides the unit's default with nothing.
for name in $(grep -Eo '^[A-Z_]+=$' "$file" | tr -d '=' || true); do
  case " $req " in *" $name "*) ;; *) echo "FAIL  $name is set but empty; delete the line to use the unit default"; bad=1;; esac
done
# SEPOLIA_RPC_URL on a node must be the logging proxy, not a credentialed endpoint.
if [ "$role" = gateway ] || [ "$role" = worker ]; then
  if grep -Eq '^SEPOLIA_RPC_URL=.*(/v[0-9]/[A-Za-z0-9_-]{16,}|[?&](key|apikey|api_key)=)' "$file"; then
    echo "FAIL  SEPOLIA_RPC_URL looks like it carries an API key; a node must use the logging proxy URL"; bad=1
  fi
fi
exit "$bad"
