#!/usr/bin/env bash
# Local liveness checks for one demo machine. Prints PASS/FAIL lines and no
# secret values. Usage: smoke.sh gateway|worker|relays|admin
# In NAT-box mode, run with NETNS=bramble-node so node-side checks run
# inside the node's network namespace.
set -uo pipefail

role="${1:-}"
fail=0
in_ns() { if [ -n "${NETNS:-}" ]; then ip netns exec "$NETNS" "$@"; else "$@"; fi; }
check() { # check <label> <command...>
  local label="$1"; shift
  if "$@" >/dev/null 2>&1; then echo "PASS  $label"; else echo "FAIL  $label"; fail=1; fi
}
http_ok() { curl -fsS --max-time 5 "$1" >/dev/null; }
node_http_ok() { in_ns curl -fsS --max-time 5 "$1" >/dev/null; }
# ss exits 0 even when nothing matches, so require at least one listener row.
node_listening() { [ -n "$(in_ns ss -H -ltn "sport = :$1" 2>/dev/null)" ]; }

sidecar_port="${BRAMBLE_SIDECAR_PORT:-7890}"
case "$role" in
  gateway)
    check "demo-service answers on loopback :${DEMO_SERVICE_PORT:-9100}" node_http_ok "http://127.0.0.1:${DEMO_SERVICE_PORT:-9100}/task"
    check "sidecar /health on :$sidecar_port" node_http_ok "http://127.0.0.1:$sidecar_port/health"
    check "status API /peers on :7899" node_http_ok "http://127.0.0.1:7899/peers"
    check "systemd: bramble-gateway active" systemctl is-active --quiet bramble-gateway
    ;;
  worker)
    check "sidecar /health on :$sidecar_port" node_http_ok "http://127.0.0.1:$sidecar_port/health"
    check "status API /peers on :7899" node_http_ok "http://127.0.0.1:7899/peers"
    check "forward listening on :${BRAMBLE_FORWARD_PORT:-8080}" node_listening "${BRAMBLE_FORWARD_PORT:-8080}"
    check "systemd: bramble-worker active" systemctl is-active --quiet bramble-worker
    ;;
  relays)
    for inst in $(systemctl list-units --plain --no-legend 'bramble-relay@*.service' | awk '{print $1}'); do
      check "systemd: $inst active" systemctl is-active --quiet "$inst"
    done
    check "relay-sidecar /health on :7891" http_ok "http://127.0.0.1:7891/health"
    check "rpc-proxy /healthz on :8545" http_ok "http://127.0.0.1:8545/healthz"
    check "console tunnel end answers on :3001" http_ok "http://127.0.0.1:3001/api/requests"
    ;;
  admin)
    check "approval service on :3001" http_ok "http://127.0.0.1:3001/api/requests"
    check "systemd: bramble-console-tunnel active" systemctl is-active --quiet bramble-console-tunnel
    ;;
  *) echo "usage: $0 gateway|worker|relays|admin" >&2; exit 2;;
esac
exit "$fail"
