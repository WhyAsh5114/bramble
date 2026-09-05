# Adjacent Work

Checked Sept 2–4, 2026. **No blockchain-based mesh VPN control plane was found.** The field is entirely conventional. That is genuine novelty, and rare.

Judges on the ENS and Ledger tracks may not know this space; a networking-literate finalist judge will.

## Direct comparisons

| What | Reality |
|---|---|
| **Tailscale** | Managed control plane on WireGuard. Distributes keys, coordinates peers, enforces ACLs. Proprietary, SaaS-only, cannot officially be self-hosted. Per-seat pricing |
| **Tailnet Lock** | Tailscale's answer to node injection: new node keys must carry a signature from a key you hold. Their GA post concedes that on device additions the coordination server sends the current signing-key state, and a compromised server could send a malicious initial state. **The single most important citation in the project** — this is the strongest existing precedent for the admission problem this project solves |
| **Headscale** | Open-source reimplementation of Tailscale's coordination server, BSD-3, v0.29.1 (June 2026), official clients, narrow single-tailnet scope, no official web UI, no Tailnet Lock per current comparisons |
| **NetBird** | Fully open, client and control plane self-hostable, embedded IdP plus OIDC to Keycloak/Authentik/Entra/Okta, group-based policies, kernel WireGuard, v0.74.x (July 2026). Heaviest to run: management, signal, and relay services plus an IdP |
| **Nebula** | Distributed control-plane alternative, certificate-based. Its CA is a file you protect; this project's authority is an on-chain registry with delegated roles and independent verification |
| **Dragonscale** | Deployment kit packaging Headscale with Google OAuth onboarding. Not a competitor — evidence that "make self-hosted mesh easier" is an active need |

## The passkey objection

The strongest objection this project faces — a pinned admin key plus signed node keys, verified at the peer, no chain needed — is essentially Tailnet Lock already. See `10_JUDGING.md` objection 5 for the full answer (bootstrap, non-equivocation, unwithholdable revocation).

## Related-but-different

**VEIL VPN** (ETHGlobal Cannes 2026 finalist) is a pay-as-you-go privacy VPN that proves no logs are kept. Different product category: exit-node privacy, not mesh connectivity and access control. If it comes up, draw that line cleanly — do not let a judge conflate them, and do not dismiss it.

**dVPN projects** (Sentinel, Orchid, Mysterium) are privacy/exit-node networks. Same distinction.
