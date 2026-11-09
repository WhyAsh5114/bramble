# Why This Exists

## The mesh VPN landscape, and the gap

WireGuard is a protocol: it encrypts and moves packets. Tailscale, Headscale, and NetBird are **control planes** on top of it, handling key distribution, peer discovery, NAT traversal, and access policy. The real decision is who runs your control plane.

| Option | What you get | What it costs |
|---|---|---|
| **Tailscale** | Nothing to run, best client experience | Per-seat pricing, and a proprietary coordination server that could inject nodes into your network |
| **Headscale** | Free, unlimited devices, official Tailscale clients | You operate a stateful server forever. Single-tailnet scope. No Tailnet Lock |
| **NetBird** | Fully open, self-hostable, built-in IdP and group policies | Heaviest to run: management, signal, and relay services plus an IdP |
| **This** | Nothing you have to run, no seats, no party who can inject a device | Narrower features, weaker NAT traversal, small ecosystem |

## The two real problems

**1. Admission is controlled by whoever runs the coordinator.**

Tailscale states the risk in their own documentation: the coordination server distributes public keys, and a malicious one could stealthily insert new nodes, at which point encryption does not help because the peer itself is hostile. They built **Tailnet Lock** for this, requiring new node keys to carry a signature from a key you hold. But their own GA post concedes the bootstrap hole: on device additions the coordination server sends the current signing-key state, and a compromised server could send a malicious initial state pointing at attacker-controlled keys. Trust on first use.

Headscale, per current comparisons, does not have Tailnet Lock at all. So self-hosters have a *worse* admission story than Tailscale customers.

**2. Self-hosting swaps a bill for an operational burden.**

Headscale is a server that must stay alive. When it is down: no enrollment, no ACL propagation, and reconnects after a network change fail. Someone is on call for the VPN. NetBird is heavier still. Neither is free in the sense that matters.

## What this offers that neither does

The coordinator does three jobs. Separate them:

- **Authorization** (which keys may join) — low-churn, needs to be tamper-evident and non-equivocating. Put it in ENSv2.
- **Key distribution** — derived from authorization. Same place.
- **Endpoint discovery and relay** — high-churn, but needs **no trust at all**, because a relay only ever sees encrypted WireGuard packets. It can drop or delay traffic; it cannot read it or impersonate anyone. So relays can be commodity, interchangeable, and permissionless.

That split is the whole idea. It is why a permissionless relay market is safe when a permissionless coordinator would not be.

**Result:** no seats, no server you keep alive, and no party who can add a device to your network. A compromised or malicious relay cannot admit anything, because admission is verified at each peer against a registry the relay does not control.

## Who this is for

Small and mid-size distributed teams and self-hosters who have outgrown the free tier and do not want to babysit Headscale. Contractors rotating on and off, home labs, VPSes, CI runners.

**And, increasingly, AI agents.** An agent on a rented VPS or CI runner needs to reach something private: an internal API, a database, a home lab service. Today you either hand it a long-lived credential it can leak or have prompt-injected out of it, or you expose the service publicly. A mesh gives the agent a **network position instead of a secret**: its own subname, its own node key, an ACL limiting it to one port on one host, an expiry, and revocation every peer verifies independently. The node key is worthless unless the registry authorizes it.

## Who this is NOT for, and say so

**Large enterprises.** They do not pay Tailscale for the mesh. They pay for SSO/SCIM, audit logs, compliance attestations, posture checks, and a support number. Removing the coordinator removes the audit trail their auditors want, and seat cost is a rounding error next to their compliance apparatus. **Never claim "cheaper than Tailscale at enterprise scale."** It is the sentence that gets the project dismissed.

## Honest cost caveat

No coordinator means weaker NAT traversal, which means more connections fall back to relays, which means more relay bytes to pay for. The cheapest case is also the most fragile one. Measure the fallback rate before making any savings claim.
