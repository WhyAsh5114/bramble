#!/usr/bin/env bash
# Two-NAT lab on one Linux machine, using network namespaces (root required).
# Topology (all addresses are documentation ranges or private; nothing leaves the machine):
#
#   nodeA 10.1.0.2 -- natA (10.1.0.1 | 192.0.2.2) --\
#                                                     inet (192.0.2.1, .5, .9) -- server 192.0.2.10
#   nodeB 10.2.0.2 -- natB (10.2.0.1 | 192.0.2.6) --/
#
# `server` stands for the relay and STUN host. It answers on 192.0.2.10 and
# 192.0.2.20 (a /32 on its lo) so the NAT probe can reach two destinations
# through the same NAT boundary. Run brambled for node A and B
# with `ip netns exec nodeA ...` and `ip netns exec nodeB ...`.
#
# Uplink: `inet` also has a veth to the host (198.18.0.0/30), and the host
# source-NATs the lab's 192.0.2.0/24 addresses to the internet. Nodes then reach
# the Sepolia proxy by name; the host routes 192.0.2.0/24 back via inet-up; resolv.conf files are written for nodeA/nodeB/server.
# Set NETLAB_UPLINK=0 for a fully isolated lab (nodes then need test fixtures
# for ENS reads). NETLAB_DNS overrides the resolvers (default 1.1.1.1 9.9.9.9).
#
# Usage: netns-lab.sh up [cone|symmetric] | block-direct | unblock-direct | status | down
set -euo pipefail

NSES="nodeA natA inet natB nodeB server"
UPLINK="${NETLAB_UPLINK:-1}"
DNS_SERVERS="${NETLAB_DNS:-1.1.1.1 9.9.9.9}"
HOST_TABLE=bramble_lab_host
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
cmd="${1:-}"

link() { # link <nsX> <ifX> <addrX/len> <nsY> <ifY> <addrY/len>
  ip link add "$2" type veth peer name "$5"
  ip link set "$2" netns "$1"; ip link set "$5" netns "$4"
  ip netns exec "$1" ip addr add "$3" dev "$2"; ip netns exec "$1" ip link set "$2" up
  ip netns exec "$4" ip addr add "$6" dev "$5"; ip netns exec "$4" ip link set "$5" up
}
nat_rules() { # nat_rules <wan-if> <lan-subnet> <mode>
  local act="masquerade"; [ "$3" = symmetric ] && act="masquerade random"
  cat <<RULES
table ip lab_nat {
  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;
    ip saddr $2 oifname "$1" $act
  }
}
RULES
}
host_rules() {
  cat <<'RULES'
table ip bramble_lab_host {
  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;
    ip saddr 192.0.2.0/24 oifname != "lab-up" masquerade
  }
  chain forward {
    type filter hook forward priority filter - 1; policy accept;
    iifname "lab-up" accept
    oifname "lab-up" accept
  }
}
RULES
}
block_rules() {
  cat <<'RULES'
table ip lab_block {
  chain forward {
    type filter hook forward priority filter; policy accept;
    ip saddr 192.0.2.2 ip daddr 192.0.2.6 meta l4proto udp drop
    ip saddr 192.0.2.6 ip daddr 192.0.2.2 meta l4proto udp drop
  }
}
RULES
}

case "$cmd" in
  up)
    mode="${2:-cone}"
    case "$mode" in cone|symmetric) ;; *) echo "mode must be cone or symmetric" >&2; exit 2;; esac
    for n in $NSES; do ip netns add "$n"; ip netns exec "$n" ip link set lo up; done
    link nodeA a-n 10.1.0.2/24 natA n-a 10.1.0.1/24
    link natA  a-i 192.0.2.2/30 inet i-a 192.0.2.1/30
    link nodeB b-n 10.2.0.2/24 natB n-b 10.2.0.1/24
    link natB  b-i 192.0.2.6/30 inet i-b 192.0.2.5/30
    link server s-i 192.0.2.10/30 inet i-s 192.0.2.9/30
    ip netns exec nodeA ip route add default via 10.1.0.1
    ip netns exec nodeB ip route add default via 10.2.0.1
    ip netns exec natA ip route add default via 192.0.2.1
    ip netns exec natB ip route add default via 192.0.2.5
    ip netns exec server ip route add default via 192.0.2.9
    ip netns exec server ip addr add 192.0.2.20/32 dev lo
    ip netns exec inet ip route add 192.0.2.20/32 via 192.0.2.10
    for n in natA natB inet; do ip netns exec "$n" sysctl -qw net.ipv4.ip_forward=1; done
    nat_rules a-i 10.1.0.0/24 "$mode" | ip netns exec natA nft -f -
    nat_rules b-i 10.2.0.0/24 "$mode" | ip netns exec natB nft -f -
    if [ "$UPLINK" = 1 ]; then
      ip link add lab-up type veth peer name inet-up
      ip link set inet-up netns inet
      ip addr add 198.18.0.1/30 dev lab-up; ip link set lab-up up
      ip netns exec inet ip addr add 198.18.0.2/30 dev inet-up; ip netns exec inet ip link set inet-up up
      ip netns exec inet ip route add default via 198.18.0.1
      ip route add 192.0.2.0/24 via 198.18.0.2 dev lab-up
      sysctl -qw net.ipv4.ip_forward=1
      host_rules | nft -f -
      for n in nodeA nodeB server; do
        install -d "/etc/netns/$n"
        for d in $DNS_SERVERS; do echo "nameserver $d"; done > "/etc/netns/$n/resolv.conf"
      done
    fi
    echo "lab up, NAT mode $mode, uplink $UPLINK. Server (relay + STUN) addresses: 192.0.2.10 and 192.0.2.20"
    ;;
  block-direct)
    block_rules | ip netns exec inet nft -f -
    echo "UDP between 192.0.2.2 (natA) and 192.0.2.6 (natB) is dropped; the server stays reachable"
    ;;
  unblock-direct)
    ip netns exec inet nft delete table ip lab_block
    ;;
  status)
    for n in $NSES; do echo "== $n"; ip netns exec "$n" ip -brief addr | grep -v '^lo'; done
    ;;
  down)
    for n in $NSES; do ip netns del "$n" 2>/dev/null || true; done
    nft delete table ip $HOST_TABLE 2>/dev/null || true
    ip link del lab-up 2>/dev/null || true
    for n in nodeA nodeB server; do rm -f "/etc/netns/$n/resolv.conf"; rmdir "/etc/netns/$n" 2>/dev/null || true; done
    echo "lab removed"
    ;;
  *) sed -n '2,20p' "$0" >&2; exit 2;;
esac
