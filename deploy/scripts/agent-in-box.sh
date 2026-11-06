#!/usr/bin/env bash
# Run a command as the unprivileged agent user inside the NAT-box namespace, so
# the agent sees the worker node's loopback forward (127.0.0.1:8080). Needed
# only when the worker node runs under the nat-box.conf drop-in.
# Enters the namespace as root, then drops to the agent user with no ambient
# capabilities and no_new_privs.
# Usage (root): agent-in-box.sh <command> [args...]
#   example: agent-in-box.sh claude
# Environment: AGENT_USER (default agent), NATBOX_NS (default bramble-node).
set -euo pipefail

user="${AGENT_USER:-agent}"
ns="${NATBOX_NS:-bramble-node}"
[ "$#" -ge 1 ] || { sed -n '2,10p' "$0" >&2; exit 2; }
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
id "$user" >/dev/null 2>&1 || { echo "no such user: $user" >&2; exit 1; }
[ -e "/run/netns/$ns" ] || { echo "namespace $ns is not up; run nat-box.sh up first" >&2; exit 1; }

home="$(getent passwd "$user" | cut -d: -f6)"
# `ip netns exec` also binds /etc/netns/$ns/resolv.conf, so the agent has DNS.
exec ip netns exec "$ns" setpriv --reuid "$user" --regid "$user" --init-groups \
  --inh-caps=-all --ambient-caps=-all --bounding-set=-all --no-new-privs -- \
  env -i HOME="$home" USER="$user" LOGNAME="$user" PATH="$PATH" TERM="${TERM:-xterm}" "$@"
