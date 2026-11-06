# ETH-38 simulated NAT plan

This test fixture supports W8, demo beat 4. It does not change the node runtime
or prove traversal on real networks. The originating request is ETH-38: build
a rootless userspace simulated NAT for STUN, hole punching, and fallback tests.

The public seam is `net.PacketConn`. Tests send opaque datagrams through real
loopback UDP sockets and observe their source addresses and replies. The
`nattest` build tag excludes the importable fixture from production builds.

| NAT type        | Mapping                                                      | Inbound filtering                        |
| --------------- | ------------------------------------------------------------ | ---------------------------------------- |
| Full cone       | One public socket per private socket                         | Any source                               |
| Restricted cone | One public socket per private socket                         | Previously contacted source IP, any port |
| Symmetric       | One public socket per private socket and destination IP:port | That destination IP:port only            |

Each simulated NAT uses a distinct loopback IP. Private sockets have synthetic
LAN addresses. Peer-IP blocking drops traffic in both directions while leaving
STUN and relay IPs reachable. No mapping expires until its private socket closes.

Implementation slices:

1. Full-cone round trip, stable mapping, and unsolicited inbound self-test.
2. Restricted-cone and symmetric mapping/filtering self-tests.
3. Two-NAT discovery/probe scenarios, peer blocking and relay recovery, socket
   deadlines, concurrent use, and cleanup checks.
4. Usage instructions, AI attribution, required repository checks, PR, and
   cross-family review issue (Net-Claude per Atlas's continuation direction). Merge remains outside this implementation run.

Tests assert behavior through the public packet interface, including observed
source addresses. They do not inspect mapping tables. A local address-reflection
fixture provides STUN-shaped discovery; production STUN codecs and automatic
WireGuard fallback remain the responsibilities of ETH-36 and ETH-39.
