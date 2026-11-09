# ADR 0019 — User-driven tailnets: one binary, wallet-owned tailnets, a free opaque relay

**Status:** Decided 9 Nov 2026. Supersedes the hosted parts of ADR 0011 (approval service placement), ADR 0013 (Tor transport), ADR 0015 (cloud demo topology) and ADR 0018 (sponsored relay sessions). Retires x402 metering on every connection (ADR 0007, ADR 0008) as a requirement.

## Context

The Mumbai build was a demo. A hosted approval console signed every enrollment and grant with the operator's Sepolia key. The operator also paid for relay sessions, and a Bun sidecar read ENS over Tor. Nobody could run a tailnet without the operator's infrastructure and money.

The project now targets people who want to try it. They should need only the binary, a browser wallet and Sepolia test ETH.

## Decision

### What the project hosts

Only two things, both replaceable:

1. A landing page that serves `install.sh` and the public manifest. Release archives come from GitHub Releases.
2. One free relay (rendezvous, data relay and STUN). Anyone can run another relay and list it in the relay directory.

Nothing else. No console, no join mailbox, no RPC proxy and no gas sponsorship.

### One binary, one UI

`bramble` contains the node (WireGuard, admission, traversal), the registry cache and an embedded web UI on `127.0.0.1`. The first screen offers **Create a tailnet** or **Join a tailnet**. Every device runs the same UI. An admin is a peer whose connected browser wallet holds roles on the tailnet registry, so admin controls appear only for that wallet.

### Tailnets are owned by wallets

A tailnet is a label in a shared, permissionless "nets" registry (first come, first served), pointing to the creator's own `UserRegistry` and `PermissionedResolver`. The creator's wallet signs and pays for every write. The project's registrar contract never holds roles on a user's registry.

A full `.eth` name stays possible for users who want one. It needs the ENS registrar's 60-second commit-reveal and a MockUSDC payment, which anyone can mint on Sepolia.

### Joining needs no server

1. The admin shares an invite (the tailnet name).
2. The joining device generates its key and shows a join code: label, WireGuard public key and a short fingerprint.
3. The admin pastes the code into their UI and signs the enrollment with their wallet.
4. The joining device sees its own record in the registry cache and connects.

The two people move the invite and the code over any channel they already use. Both screens show the same fingerprint, so a substituted code is visible.

### Registry reads come from a local event cache

Each node keeps its own cache of its tailnet registry, the resolvers it points to and the relay directory. It applies `eth_getLogs` results incrementally and stores the confirmed state on disk. Every poll re-applies the unconfirmed tail on top of the confirmed state, so a reorg cannot leave stale state behind. One poll costs a constant number of RPC calls, whatever the member count. The node fails over across several public RPC endpoints, and the user can set their own.

The cache is per node by design. A shared hosted indexer would be a trusted coordinator.

### Relays are permissionless and opaque

- **Directory.** Any operator registers a relay label in the shared "relays" registry and publishes its records (rendezvous address, data relay range, STUN address, price) on its own resolver.
- **Sealed signaling.** Peers encrypt and authenticate candidate messages with keys derived from their WireGuard static keys. A relay routes by a mailbox ID derived from the key pair and sees neither the candidates nor the device keys.
- **Bound sessions.** A data relay session accepts only senders that prove the session secret, so a stranger cannot take over a session's port.
- **Caps.** A relay enforces per-session byte caps, per-source rate limits and a daily and monthly byte budget. When the budget runs out, it stops forwarding data and keeps serving rendezvous.
- **Failure handling.** Clients prefer direct paths, use at least two rendezvous relays when available, score relays on health and keep a local blocklist. A malicious relay can slow a connection or refuse service. It cannot read traffic, impersonate a peer or admit a device.

### Exposing services

A device exposes local ports as named services from its UI. A service is either open to every tailnet member or limited to devices with an ACL grant (ADR 0005 digests, computed by the admin's own device). Each device exposes a built-in `ping` service to the whole tailnet by default; `--no-ping` turns it off.

## Invariants kept

1. No central enforcement. Peers check admission and ACLs themselves.
2. No member topology on-chain. Relays publish addresses on purpose; devices never do.
3. Access records are digests, never plaintext names, hosts or ports.

## Invariant relaxed

`brambled` may write its registry cache to disk. `pnpm check:no-local-state` allows the cache directory only.

## Consequences

- Tor, Hedera payments, the HCS audit trail, the approval console and the read-only dashboard leave the main tree. The `archive/hackathon` tag keeps them.
- A tailnet costs its creator Sepolia gas only.
- The project's relay can run out of budget. Users can run their own relay.
