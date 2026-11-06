# W8a STUN binding reference

Source task: ETH-36, demo beat 4. The human-authored task requires RFC 5389
binding discovery from the WireGuard UDP socket, a configurable server list,
no local-state writes, fake-server unit tests, and rootless status API evidence.
`docs/15_MUMBAI_HANDOFF.md` governs the implementation.

When STUN is enabled (`wgnode.Config.EnableSTUN`), the implementation wraps
wireguard-go's default bind. Disabled nodes retain the unwrapped default bind. WireGuard remains the
socket reader. The wrapper consumes STUN packets and leaves other packet slots
unchanged. Requests use the wrapped bind's `Send`; no second UDP socket is opened.
Responses must match the random 96-bit transaction ID and server address.
Malformed responses are ignored. Valid server errors terminate the transaction.
IPv4 and IPv6 XOR-MAPPED-ADDRESS and legacy MAPPED-ADDRESS are supported.
Unknown required attributes and invalid fingerprints are rejected.

The client sends up to seven copies of a transaction with an initial 500 ms
retry interval, exponential backoff, and an 8 s final wait. DNS resolution has a
5 s limit; each address has a 40 s transaction limit. Context cancellation and
bind close interrupt transactions. Server names require explicit numeric ports.
Servers and resolved addresses are tried in order until one succeeds.

`-stun-servers` overrides `BRAMBLE_STUN_SERVERS`. Both accept a comma-separated
list of `host:port` values; bracket IPv6 literals. Empty disables discovery.
The daemon discovers in the background and refreshes 30 s after each pass.
`GET /endpoint` reports `disabled`, `discovering`, `discovered`, or `unavailable`.
A successful observation includes `endpoint`, `server`, and `observedAt`.
Starting a new pass or failing discovery clears the old endpoint.

Discovery does not grant admission, publish to ENS, change peer endpoints,
exchange candidates, punch holes, or choose relays. A mapped address is an
observation from one STUN server, not proof of peer reachability. UDP STUN
cannot use Tor; the configured server learns the asker's public IP and port.

Required checks: `pnpm verify`, `pnpm test:access-flow`, and
`go test -race ./stun ./wgnode ./statusapi` from `brambled/`.
The rootless status API test also completes a WireGuard handshake and ICMP
exchange through the same bind after discovery. Real NAT traversal remains Open.

Protocol reference: [RFC 5389](https://www.rfc-editor.org/rfc/rfc5389.html).

With STUN enabled, each send forwards a fresh destination-only endpoint so the
kernel chooses the current source and interface; the shared endpoint is not
mutated. The rationale, trade-off, and rejected alternatives are in
`docs/adr/0017-stun-bind-wrapper-source-selection.md`.
