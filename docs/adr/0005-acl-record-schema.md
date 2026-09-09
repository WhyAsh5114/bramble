# ADR 0005 — ACL record schema: symbolic service names, ECDH trust-store digests, two independent EAC roles

**Status:** Decided, Sept 7–8 2026. Both EAC roles verified end-to-end on the live hackathon Sepolia deployment (`admincli/test/acl-eac-check.ts`, `admincli/test/acl-granters-eac-check.ts`).

## Context

Phase 2 Section A (`adr/0004`) built EAC-gated enroll/rotate/revoke. Section B needed the ACL record schema and read path Gate 2.3's enforcement (Section C) will consume. Several design questions were resolved in conversation before writing code, each driven by the same concern: ENS records are permanent and public, so anything written there is a permanent public disclosure, not a private config value — and any secret this scheme relies on has to actually be distributable without falling back on a person copying a value around by hand.

**Question 1 — what shape does an ACL grant take?** An earlier draft (still visible in `03_ARCHITECTURE.md`'s original diagram, e.g. `agent-1.acme.eth → ... ACL(db:5432 only)`) recorded a destination host and port directly. Rejected: publishing `host:port` pairs on-chain broadcasts internal network topology to anyone who reads the registry. Resolved: ACL grants are **fully symbolic** — a device/agent's ACL is a list of service *names* only (e.g. `db`, `cache`). The receiving peer (in its role as a "gateway" — see below) maps a name to a local port using its own local, non-ENS config. Mesh membership (the WireGuard peer table, already gated by admission — Gate 1.1/1.2) restricts *which hosts* a peer can reach at all; the ACL is an additional restriction on top of that.

**Question 2 — does even the service *name* leak too much?** A plaintext symbolic name still ties a public identity to a plaintext capability forever, publicly. Resolved: publish a digest, not the name.

**Question 3 — a bare hash is dictionary-attackable.** `keccak256("db")` doesn't hide anything: the space of plausible service names is small, so anyone can hash every candidate and match against the public record. The fix needs to be a *keyed* function — but the key needs a distribution story that doesn't just relocate the problem.

**Question 4 — a manually-shared symmetric secret (a "pepper") is the wrong fix.** An HMAC keyed by one secret value distributed by hand to every verifier works cryptographically, but reintroduces exactly the kind of manual, out-of-band trust operators were trying to avoid, and — worse — concentrates real power: whoever holds that one pepper can vouch for grants on every gateway in the tailnet, which doesn't match this project's actual EAC philosophy of independent, per-resource-owner delegation (`adr/0004`'s whole point).

**Resolved (final): reuse each identity's already-published WireGuard (X25519) pubkey as the PKI substrate, via pairwise ECDH, with each gateway independently choosing whose vouches it trusts.** No new keypair, no manually-copied secret, and no single party with tailnet-wide authority over ACL grants.

## Design

### Term: what "gateway" means here

Not a separate component, and not a relay. A "gateway" is simply **whichever mesh member is being asked to `CONNECT` to one of its own locally-offered services** — i.e. `brambled` itself, running on that device, doing one more thing (Section C, not built yet) on top of what it already does (Gate 1.1/1.2's admission loop). Any enrolled device can be a gateway for whatever it locally chooses to expose. Rendezvous/data relays (`adr/0003`) are uninvolved — they only ever forward opaque bytes and are never part of ACL enforcement.

### Record placement: two independent records, two independent owners

1. **`acl`**, on the *requesting device's own* resolver — the device or agent whose capabilities are being restricted. Comma-separated list of digests (never plaintext service names). EAC-gated by `ROLE_SET_TEXT` scoped to the `acl` setter (`grantSetterRoles`, `adr/0004`'s pattern, `admincli/src/roles.ts`'s `aclSetter()`). Controls **who may write anything about this device at all** — the device owner's consent/anti-spam boundary.
2. **`acl-granters`**, on the *gateway's own* resolver — the list of already-enrolled identity labels that gateway accepts ACL vouches from. EAC-gated by its own role, `ROLE_SET_TEXT` scoped to the `acl-granters` setter (`aclGrantersSetter()`), independently grantable, controlled entirely by whoever owns/deployed that specific gateway's resolver. Controls **whose digests this device, in its gateway role, actually honors.**

These are deliberately two separate trust decisions, each owned by a different resource's own EAC roles — nobody, including "the admin," has unilateral authority over both sides of a grant. A digest written into a device's `acl` record only matters if the target gateway's own `acl-granters` list happens to include the identity that produced it; an untrusted granter's digest just silently never matches anything, the same way an unrecognized signer's signature just fails to verify in any PKI trust-store model. This generalizes cleanly to multiple independent operators: each gateway owner decides who they trust, with no tailnet-wide admin required.

### Digest algorithm: pairwise ECDH between already-existing identity keys

Every enrolled ENS name — device, agent, or "granter" (a granter is *just* an enrolled name; no special identity type exists) — already publishes an X25519 pubkey via its `pubkey` text record, since that's the same key WireGuard uses for the Noise_IK handshake (`brambled/wgnode`, `golang.org/x/crypto/curve25519`, RFC 7748-clamped). This is the whole PKI substrate needed: no new keypair, no new published record for identity.

To grant "device D may reach service `db` on gateway G," vouched for by already-enrolled granter identity A:

```
sharedSecret = X25519(A's private key, G's already-published pubkey)
aclKey       = HKDF-SHA256(ikm=sharedSecret, salt=none, info="bramble-acl-v1", length=32)
digest       = hex(HMAC-SHA256(key=aclKey, message=utf8(trim(lowercase("db")))))
```

`admincli/src/roles.ts`'s `aclDigestECDH()` is the reference implementation (`@noble/curves`'s `x25519`, `@noble/hashes`'s `hkdf`/`hmac`/`sha256` — already transitive dependencies via `viem`, pinned directly here). The digest is appended to D's `acl` record via `admincli/src/set-acl.ts`.

Gateway G later verifies without anything being transmitted: it reads its own `acl-granters` list, and for each trusted granter label, computes `X25519(G's own private key, that granter's already-published pubkey)` — by Diffie-Hellman symmetry, **identical** to what A computed — derives the same `aclKey`, and checks whether the resulting digest for the requested service name appears in D's `acl` record. HKDF domain-separates this from WireGuard's own Noise_IK use of the same raw key, since reusing a raw ECDH output across two unrelated protocols is exactly the failure mode HKDF exists to prevent.

**Why this doesn't concentrate power the way a shared pepper did:** nothing is transmitted or copied by hand — a gateway independently rederives the identical shared secret from public data plus its own private key, exactly like two ordinary PKI peers agreeing on a session key from nothing but each other's public keys. And unlike a single tailnet-wide pepper, no one identity's key lets you forge a grant a gateway that doesn't trust you will accept: each gateway's own `acl-granters` list is the actual authorization boundary, owned by that gateway alone.

**Why this doesn't reintroduce a trusted coordinator (Gate 1.1/1.2):** issuing a grant (who may write `acl`/`acl-granters`, and which granter identity a gateway trusts) has always been an administrative function gated by EAC — enroll/rotate/revoke already work this way (`adr/0004`). Gate 1.1/1.2's actual claim is about **admission-time verification**: no party can unilaterally get a peer admitted, or a `CONNECT` approved, without the receiving peer independently checking public chain state itself. That holds exactly as before — a gateway's verification is a fully local computation (its own private key, public chain reads), with zero live call to any admin or coordinator at connection time.

**Residual leak, stated plainly:** an on-chain observer can still see *how many* distinct capabilities a device holds and *how many* granters a gateway trusts — cardinality, not content. Treated as acceptable, same spirit as Gate 1.3's documented (not hidden) slower-than-Tailscale revocation latency.

### What Section C still owns

Not built here: the gateway's single fixed-port listener, `CONNECT <service>` protocol parsing, the local (non-ENS) service-name-to-port config a gateway host is configured with, the verification loop above (iterate `acl-granters`, compute a candidate digest per trusted granter, check membership in the requester's `acl`), and a pubkey→label reverse lookup (derivable from the admission `Loop`'s already-tracked `Peers`/`knownPubkeys`) to know whose `acl` record to fetch for an inbound connection's already-authenticated WireGuard peer. Section C's Go implementation must reproduce the exact canonical algorithm above byte-for-byte — `golang.org/x/crypto/curve25519` (already imported in `brambled/wgnode`) for the ECDH, Go's stdlib `crypto/hmac`/`crypto/sha256` plus a standard HKDF implementation for the rest — no new external dependency either.

**Built — see `adr/0006-gateway-connect-protocol.md`.** One correction to the paragraph above: the pubkey→label reverse lookup turned out to be unnecessary. The static `AllowedIP → Label` mapping from `admission.Loop`'s own `-peer` config already identifies an inbound connection's caller directly, with no pubkey involved — ADR 0006 explains why that's trustworthy.

## Consequence

- `sidecar/src/ens/client.ts`'s `DeviceRecord.acl`/`.aclGranters: string[]` and `brambled/sidecar/client.go`'s `DeviceRecord.ACL`/`.ACLGranters []string` expose both lists as opaque strings — the sidecar's job stays pure resolution, not interpretation; it never computes or verifies a digest.
- `docs/03_ARCHITECTURE.md` and Gate 2.3's test description in `docs/05_BUILD_PLAN.md` are corrected to the symbolic/digest shape — the `db:5432` host:port examples they still showed described the first, rejected draft.
- `admincli/src/set-acl.ts` cross-checks the supplied `BRAMBLE_GRANTER_PRIVATE_KEY` against the claimed granter identity's on-chain pubkey before writing anything, catching the operator-error case of vouching under the wrong identity.
- A granter is not a new concept to operate — it's just another enrolled ENS name, provisioned with the same `enroll.ts` any device uses.
