#!/usr/bin/env bash
# Runs the sponsored-relay check (ETH-189) behind two symmetric NATs with no
# root on the host. A short-lived Docker container with NET_ADMIN and
# SYS_ADMIN builds the six-namespace lab from netns-lab.sh inside itself, so
# nothing is changed on the host network. Needs: docker access, go, bun.
#
#   deploy/nat-lab/docker-lab.sh
#
# What it runs (all inside the container):
#   1. netns-lab.sh up symmetric (NETLAB_UPLINK=0: no route out of the lab).
#   2. The real relay binary in the `server` namespace with -meter -data-relay
#      -sponsor-enrolled. Its sidecar is the test harness in
#      relay-sidecar/test/e2e-harness: real sponsor routes, token minting and
#      allocation, but a fixture instead of the ENS read (no chain here).
#   3. Control: nodeA and nodeB each point WireGuard straight at the other's
#      NAT address. The handshake must fail.
#   4. Relay run: nodeA ("device", key-less) and nodeB ("cloud") get sponsored
#      tokens and one sponsored session, direct UDP between the NATs is also
#      dropped (block-direct), and the device must receive HTTP 200 from a
#      server on the cloud node.
set -euo pipefail

if [ "${1:-}" != "--inside" ]; then
  repo="$(cd "$(dirname "$0")/../.." && pwd)"
  out="$(mktemp -d)"
  trap 'rm -rf "$out"' EXIT
  (cd "$repo/relay" && CGO_ENABLED=0 go build -o "$out/relay" .)
  (cd "$repo/brambled" && CGO_ENABLED=0 go test -c -o "$out/brambled.test" .)
  docker run --rm \
    --cap-add NET_ADMIN --cap-add SYS_ADMIN \
    --security-opt apparmor=unconfined --security-opt seccomp=unconfined --security-opt systempaths=unconfined \
    -v "$repo:/repo:ro" -v "$out:/out" -v "$(command -v bun):/usr/local/bin/bun:ro" \
    debian:bookworm-slim bash /repo/deploy/nat-lab/docker-lab.sh --inside
  exit $?
fi

export HOME=/tmp
apt-get update -qq >/dev/null 2>&1
apt-get install -y -qq iproute2 nftables procps >/dev/null 2>&1
LAB=/repo/deploy/nat-lab/netns-lab.sh
TESTBIN=/out/brambled.test
NETLAB_UPLINK=0 bash "$LAB" up symmetric

key_line="$(BRAMBLE_NETLAB_ROLE=keygen "$TESTBIN" -test.run '^TestNetlabKeygen$' | grep '^NETLAB_KEY')"
dev_priv="$(echo "$key_line" | awk 'NR==1{print $2}')"; dev_pub="$(echo "$key_line" | awk 'NR==1{print $3}')"
key_line="$(BRAMBLE_NETLAB_ROLE=keygen "$TESTBIN" -test.run '^TestNetlabKeygen$' | grep '^NETLAB_KEY')"
cloud_priv="$(echo "$key_line" | awk '{print $2}')"; cloud_pub="$(echo "$key_line" | awk '{print $3}')"

echo "== relay (real binary, -meter -data-relay -sponsor-enrolled) in the server namespace"
ip netns exec server env HARNESS_ENROLLED_LABELS=device1,cloud1 \
  BRAMBLE_TAILNET_NAME=fixture.eth BRAMBLE_TAILNET_REGISTRY=0x0000000000000000000000000000000000000001 \
  /out/relay -addr 192.0.2.10:9420 -meter -payee 0.0.1 \
  -sidecar-dir /repo/relay-sidecar/test/e2e-harness -sidecar-port 7891 \
  -data-relay -data-host 192.0.2.10 -internal-port 7895 -sponsor-enrolled -sponsor-bytes 1048576 \
  >/out/relay.log 2>&1 &
for _ in $(seq 60); do
  ip netns exec nodeA bash -c 'exec 3<>/dev/tcp/192.0.2.10/7891' 2>/dev/null && break
  sleep 0.5
done

role() { # role <ns> <role> <priv> <peer-pub> <label> [extra env...]
  local ns="$1" r="$2" priv="$3" peer="$4" label="$5"; shift 5
  ip netns exec "$ns" env BRAMBLE_NETLAB_ROLE="$r" NETLAB_PRIV="$priv" NETLAB_PEER_PUB="$peer" NETLAB_LABEL="$label" \
    NETLAB_RELAY_RDV=192.0.2.10:9420 NETLAB_RELAY_SIDECAR=http://192.0.2.10:7891 "$@" \
    "$TESTBIN" -test.run '^TestNetlabRole$' -test.v -test.timeout 150s
}

echo "== control: both nodes point WireGuard at the other's NAT address (symmetric NAT, no relay)"
role nodeB cloud "$cloud_priv" "$dev_pub" cloud1 NETLAB_MODE=direct-control NETLAB_DIRECT_ENDPOINT=192.0.2.2:51820 >/out/cloud-control.log 2>&1 &
cloud_pid=$!
# The control must fail to connect, against a cloud side that is up and
# listening for the whole attempt. A working direct path, a crashed device
# run, or a cloud side that never came up or died means the relay result
# below would prove nothing.
status=0
cloud_ready=0
for _ in $(seq 40); do
  if grep -q '^NETLAB_READY cloud' /out/cloud-control.log; then cloud_ready=1; break; fi
  kill -0 "$cloud_pid" 2>/dev/null || break
  sleep 0.5
done
if [ "$cloud_ready" -ne 1 ]; then
  echo "control: the cloud side did not come up; see /out/cloud-control.log" >&2
  status=1
else
  role nodeA device "$dev_priv" "$cloud_pub" device1 NETLAB_MODE=direct-control NETLAB_DIRECT_ENDPOINT=192.0.2.6:51820 >/out/device-control.log 2>&1 || status=1
  grep -E '^NETLAB|^(---|FAIL|ok|PASS)' /out/device-control.log || true
  grep -q 'NETLAB_RESULT direct-control: no handshake' /out/device-control.log || status=1
  if ! kill -0 "$cloud_pid" 2>/dev/null; then
    echo "control: the cloud side exited during the device attempt; see /out/cloud-control.log" >&2
    status=1
  fi
fi
kill "$cloud_pid" 2>/dev/null || true; wait "$cloud_pid" 2>/dev/null || true
if [ "$status" -ne 0 ]; then
  echo "control failed: the direct path connected, or one side of the control did not run to the end; see /out/*-control.log" >&2
  bash "$LAB" down >/dev/null
  exit 1
fi

echo "== relay run: direct UDP between the NATs dropped; both sides use sponsored tokens, device a sponsored session"
bash "$LAB" block-direct
role nodeB cloud "$cloud_priv" "$dev_pub" cloud1 >/out/cloud-relay.log 2>&1 &
cloud_pid=$!
role nodeA device "$dev_priv" "$cloud_pub" device1 | tee /out/device-relay.log | grep -E '^NETLAB|^(---|FAIL|ok|PASS)' || status=1
grep -q 'NETLAB_RESULT relay: HTTP 200' /out/device-relay.log || status=1
kill "$cloud_pid" 2>/dev/null || true; wait "$cloud_pid" 2>/dev/null || true

echo "== relay log (sponsorship lines)"
grep -iE 'sponsor|session|listening' /out/relay.log | head -10 || true
bash "$LAB" down >/dev/null
exit "$status"
