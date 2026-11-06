#!/usr/bin/env bash
# Build and install Bramble on one demo machine. Run as root from the repo
# checkout at /opt/bramble. Usage: install.sh gateway|worker|relays|admin
# Idempotent. Does not start services and does not write secrets.
set -euo pipefail

role="${1:-}"
case "$role" in gateway|worker|relays|admin) ;; *) echo "usage: $0 gateway|worker|relays|admin" >&2; exit 2;; esac
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }

root=/opt/bramble
[ -d "$root/.git" ] || { echo "expected a git checkout at $root" >&2; exit 1; }
cd "$root"

need() { command -v "$1" >/dev/null || { echo "missing tool: $1 ($2)" >&2; missing=1; }; }
missing=0
need git "apt install git"
need pnpm "see deploy/README.md, step 'Install the toolchain'"
need bun "see deploy/README.md, step 'Install the toolchain'"
case "$role" in
  gateway|worker|relays) need go "Go 1.27 or later";;
esac
case "$role" in
  gateway|worker) need nft "apt install nftables (the NAT box needs it)"; need python3 "apt install python3 (the STUN probe needs it)";;
esac
[ "$missing" -eq 0 ] || exit 1

id bramble >/dev/null 2>&1 || useradd --system --home-dir /var/lib/bramble --create-home --shell /usr/sbin/nologin bramble
install -d -m 0750 -o root -g bramble /etc/bramble

install_unit() { install -m 0644 "deploy/systemd/$1" "/etc/systemd/system/$1"; }
seed_env() { # seed_env <example> <target>: copy once, never overwrite, mode 0600
  if [ ! -e "$2" ]; then install -m 0600 -o bramble -g bramble "$1" "$2"; echo "created $2 (fill in values)"; fi
}

case "$role" in
  gateway)
    pnpm install --frozen-lockfile --filter @bramble/sidecar...
    (cd brambled && go build -o bin/brambled . && go build -o bin/demo-service ./cmd/demo-service)
    seed_env deploy/env/node.env.example /etc/bramble/gateway.env
    install_unit bramble-demo-service.service
    install_unit bramble-gateway.service
    install_unit bramble-nat-box.service
    ;;
  worker)
    pnpm install --frozen-lockfile --filter @bramble/sidecar...
    (cd brambled && go build -o bin/brambled .)
    seed_env deploy/env/node.env.example /etc/bramble/worker.env
    install_unit bramble-worker.service
    install_unit bramble-nat-box.service
    ;;
  relays)
    pnpm install --frozen-lockfile --filter @bramble/relay-sidecar...
    pnpm install --frozen-lockfile --filter @bramble/rpc-proxy... || echo "rpc-proxy package not in this checkout yet (ETH-44); skipped"
    (cd relay && go build -o bin/relay .)
    seed_env deploy/env/relay.env.example /etc/bramble/relay-relay1.env
    install_unit 'bramble-relay@.service'
    ;;
  admin)
    pnpm install --frozen-lockfile
    pnpm --filter @bramble/approval build || echo "approval package not in this checkout yet; skipped"
    seed_env deploy/env/admin.env.example /etc/bramble/admin.env
    install_unit bramble-approval.service
    install_unit bramble-console-tunnel.service
    ;;
esac

chown -R bramble:bramble "$root" 2>/dev/null || true
systemctl daemon-reload
echo "installed role '$role'. Next: fill /etc/bramble/*.env, run deploy/scripts/check-env.sh $role, then start units."
