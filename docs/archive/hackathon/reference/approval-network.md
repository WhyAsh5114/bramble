# Approval console network reference

**Kind:** Reference. **Demo beats:** 2 (approve), 4 (direct and relay paths), 5 (revoke).

## Authorization and data

The approval console uses the existing operator bearer token for both network
routes. The browser holds the token in tab memory. Lock and reload clear it.
`dashboard/` remains read-only. Grant and revoke services are unchanged.

`GET /api/network` builds topology from ENS members, join records and service
requests, including granted and revoked requests. It always includes a gateway,
labeled with `APPROVAL_ENS_PARENT`, the first request's `gatewayLabel`, or
`Gateway` when neither exists. Each device without a live observation links to
the gateway with `path: "unavailable"`. A grant never proves tunnel admission.

The gateway status API at `BRAMBLED_STATUS_URL` is optional enrichment. Without
it, the graph and configured console-host HTTP checks still work. Peer admission
and paths are `unavailable`; `lastHandshake` is null and renders as **Unavailable**.
No direct or relayed path is inferred from approval records or missing data.

All responses use `Cache-Control: no-store`. The response contains `state`,
`relayState`, `membershipState`, `observedAt`, `nodes` and `links`. Source states
are `ok`, `not_configured` or `unavailable`. `state` describes the optional gateway
feed, so `not_configured` still contains approval-derived topology. Node fields
include `id`, `label`, `kind`, `admission`, optional `membership`, `grants`,
`lastHandshake`, `path`, `pingable` and optional `probeReason`. Each grant entry
identifies a request, service, gateway and request state. Pending requests do not
replace active grants. Unavailable probes carry a reason beside the disabled button.

The topology response omits public keys, private member endpoints, allowed IPs,
mesh addresses and relay URLs. Unauthenticated callers receive `401`; missing
token configuration returns `503`. Upstream errors use fixed messages.

## Optional live observations

When configured, the server reads `/peers` and `/relays` from the gateway. These
observations describe that gateway's perspective, not the whole mesh. An admitted
peer whose endpoint host matches a discovered relay host has a `relayed` link
through that relay. With a complete relay table, an unmatched observed endpoint
is `direct`. Missing endpoints or unavailable relay discovery produce `unknown`.
Non-admitted observed peers have no observed link. Devices with no observation
retain their approval-derived `unavailable` links.

The server resolves relay DNS names with bounded A/AAAA lookups. Failed relay
resolution keeps unmatched observed paths `unknown`. Host matching cannot
distinguish a direct peer colocated on a relay host from a relay allocation.
A line never proves reachability. Handshake timestamps are the gateway's last
recorded WireGuard handshake, not a freshness guarantee.

The UI polls every five seconds. Failed refreshes label the last snapshot stale
and pause probes. Initial failed observations still retain approval-derived nodes.
The diagram shows up to 24 nodes; the labeled node list retains every returned
node. Solid, dashed and dotted links and text labels identify paths without
relying on color. The node list provides state, timestamps and keyboard-operated
probe buttons.

## Console-host probes

The approval process reads the existing deployment configuration:

| Variable                         | Format                                              | Probe target                                                |
| -------------------------------- | --------------------------------------------------- | ----------------------------------------------------------- |
| `BRAMBLE_DATA_RELAYS`            | Comma-separated `label=http(s)://host:port` entries | Each configured relay's `/health`                           |
| `BRAMBLE_RENDEZVOUS_PAYMENT_URL` | One HTTP(S) origin                                  | Rendezvous service's `/health`, node ID `relay:rendezvous`  |
| `BRAMBLED_STATUS_URL`            | Optional gateway status origin                      | Enriches topology and enables observed-device tunnel probes |

Only server configuration and optional gateway discovery provide targets. ENS
namespace and grant labels identify the gateway but do not supply a network
address or a supported probe protocol. Member mesh addresses do not identify a
TCP port or HTTP service. These nodes retain disabled buttons with explicit
reasons; the server does not invent probe targets or claim a latency measurement.
UDP rendezvous addresses alone do not enable an HTTP check.

`POST /api/network/ping` takes a known node `id`, such as
`{"id":"relay:relay-eu"}`. It rejects arbitrary addresses and unknown identifiers.
The client cannot provide a target URL. Configured relay and rendezvous probes
work without reading the gateway feed. Invalid URLs, credentials, queries,
fragments and non-HTTP schemes are rejected. Redirects are not followed. Requests
time out after three seconds and JSON responses are limited to one megabyte.

The console host measures the HTTP `/health` request and bounded JSON response.
The service must return `{"ok":true}`. The UI labels this **HTTP RTT**. This
measurement does not prove tunnel connectivity, WireGuard forwarding,
device-to-relay latency, or successful access to a private service.

With optional live observations, the approval server can also forward an
admitted tracked device's exact label to the gateway's existing `/ping` endpoint.
This runs an ICMP echo over the gateway tunnel and is labeled **tunnel RTT**.

Both probe responses contain `reachable`, nullable `rttMs`, `method` (`icmp` or
`http`), `measuredAt` and a fixed `detail`. No reply or a timed-out probe is a
failure, not proof of revoked access. ICMP can be blocked independently of access.
The UI records the timestamp and clears the prior result on retry.

## Verification scope

API tests exercise the real HTTP handler with `network` omitted and
`BRAMBLED_STATUS_URL` unset. They use a real loopback HTTP service for relay and
rendezvous success and failure, arbitrary-target rejection and response redaction.
Other cases cover ENS membership, join records, approved/revoked requests and
optional synthetic gateway observations.

Browser tests exercise the real approval HTTP handler with an in-memory chain.
The no-feed case covers approval-derived links, unavailable fields, disabled
member probes, console-host HTTP probe success/failure and timestamps. Axe scans
and screenshots cover that state at 1280 and 390 CSS pixels in both themes.
Existing cases cover keyboard decisions, stale data and explicit 4.5:1 text and
3:1 control/focus palette assertions. Automated scans and fixtures do not establish
live cloud connectivity or complete manual WCAG conformance.

Run the package build, then `pnpm --filter @bramble/approval test:ui`. Alternate
ports use `BRAMBLE_TEST_UI_PORT` and `BRAMBLE_TEST_API_PORT`. Set
`PLAYWRIGHT_BROWSERS_PATH` when the browser installation uses a shared cache.
