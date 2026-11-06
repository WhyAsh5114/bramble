#!/usr/bin/env bash
# NAT-in-a-box: put the Bramble node behind its own NAT on the same machine.
# The host becomes the NAT router; the node runs in a network namespace with a
# private address and reaches the internet only through source NAT. This gives
# each demo machine its own NAT without extra cloud machines (W8, demo beat 4).
#
# Usage (root):
#   nat-box.sh up [cone|symmetric]   create namespace, veth pair, NAT rules
#   nat-box.sh block-direct <ip>     drop the node's UDP to <ip> (the other node's public IP)
#   nat-box.sh unblock-direct        remove the block
#   nat-box.sh status
#   nat-box.sh print-rules <mode> [ip]   print the nftables rules; changes nothing
#   nat-box.sh down
#
# cone      source NAT that keeps the source port when free (port-restricted cone behaviour)
# symmetric source NAT with a random source port per flow (symmetric behaviour)
set -euo pipefail

NS="${NATBOX_NS:-bramble-node}"
HOST_IF=bnb-host
NODE_IF=bnb-node
HOST_ADDR=10.200.0.1
NODE_ADDR=10.200.0.2
SUBNET=10.200.0.0/24
TABLE=bramble_natbox
DNS_SERVERS="${NATBOX_DNS:-1.1.1.1 9.9.9.9}"

cmd="${1:-}"

rules() { # rules <mode> [block-ip]
  local nat="masquerade" block=""
  [ "$1" = symmetric ] && nat="masquerade random"
  [ -n "${2:-}" ] && block="iifname \"$HOST_IF\" ip daddr $2 meta l4proto udp drop"
  cat <<RULES
table ip $TABLE {
  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;
    ip saddr $SUBNET oifname != "$HOST_IF" $nat
  }
  chain forward {
    type filter hook forward priority filter - 1; policy accept;
    $block
  }
}
RULES
}

if [ "$cmd" != print-rules ] && [ "$(id -u)" -ne 0 ]; then echo "run as root" >&2; exit 1; fi

case "$cmd" in
  print-rules) # print-rules <cone|symmetric> [blocked-ip]: no root, no changes; for review and `nft -c`
    rules "${2:-cone}" "${3:-}"
    ;;
  up)
    mode="${2:-cone}"
    case "$mode" in cone|symmetric) ;; *) echo "mode must be cone or symmetric" >&2; exit 2;; esac
    ip netns list | grep -q "^$NS\b" || ip netns add "$NS"
    if ! ip link show "$HOST_IF" >/dev/null 2>&1; then
      ip link add "$HOST_IF" type veth peer name "$NODE_IF"
      ip link set "$NODE_IF" netns "$NS"
    fi
    ip addr show dev "$HOST_IF" | grep -q "$HOST_ADDR/" || ip addr add "$HOST_ADDR/24" dev "$HOST_IF"
    ip link set "$HOST_IF" up
    ip netns exec "$NS" ip addr show dev "$NODE_IF" | grep -q "$NODE_ADDR/" || ip netns exec "$NS" ip addr add "$NODE_ADDR/24" dev "$NODE_IF"
    ip netns exec "$NS" ip link set "$NODE_IF" up
    ip netns exec "$NS" ip link set lo up
    ip netns exec "$NS" ip route replace default via "$HOST_ADDR"
    sysctl -qw net.ipv4.ip_forward=1
    install -d "/etc/netns/$NS"
    for s in $DNS_SERVERS; do echo "nameserver $s"; done > "/etc/netns/$NS/resolv.conf"
    nft delete table ip "$TABLE" 2>/dev/null || true
    rules "$mode" | nft -f -
    echo "$mode" > "/run/$TABLE.mode"
    echo "NAT box up: namespace $NS, mode $mode, node address $NODE_ADDR"
    ;;
  block-direct)
    ip="${2:?usage: nat-box.sh block-direct <ip>}"
    mode="$(cat /run/$TABLE.mode 2>/dev/null || echo cone)"
    nft delete table ip "$TABLE" 2>/dev/null || true
    rules "$mode" "$ip" | nft -f -
    echo "$ip" > "/run/$TABLE.block"
    echo "direct UDP from the node to $ip is now dropped"
    ;;
  unblock-direct)
    mode="$(cat /run/$TABLE.mode 2>/dev/null || echo cone)"
    nft delete table ip "$TABLE" 2>/dev/null || true
    rules "$mode" | nft -f -
    rm -f "/run/$TABLE.block"
    echo "direct UDP block removed"
    ;;
  status)
    echo "mode:  $(cat /run/$TABLE.mode 2>/dev/null || echo 'not up')"
    echo "block: $(cat /run/$TABLE.block 2>/dev/null || echo none)"
    ip netns exec "$NS" ip -brief addr 2>/dev/null || echo "namespace $NS missing"
    nft list table ip "$TABLE" 2>/dev/null || echo "nft table $TABLE missing"
    ;;
  down)
    nft delete table ip "$TABLE" 2>/dev/null || true
    ip link del "$HOST_IF" 2>/dev/null || true
    ip netns del "$NS" 2>/dev/null || true
    rm -f "/run/$TABLE.mode" "/run/$TABLE.block"
    echo "NAT box removed"
    ;;
  *) sed -n '2,19p' "$0" >&2; exit 2;;
esac
