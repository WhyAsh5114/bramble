# ADR 0001 — Split Ledger key custody (`ring`) from Ledger signing (`send`)

**Status:** Decided, Sept 4 2026. Verified against `@ledgerhq/wallet-cli` v1.0.1 docs.

## Context

The Ledger track requires building on the Ledger Agent Stack, specifically `wallet-cli ring`. It would be easy to assume `ring` is a general-purpose signing tool, since Ledger devices are best known for signing. It is not: `ring init / encrypt / decrypt / keys / destroy` is LKRP-backed **encryption** of files and text, keyed to wallet-sync membership. It does not sign transactions.

Two distinct jobs exist in this project that both touch the Ledger:

1. Storing a headless node's WireGuard private key at rest on a host with no Ledger attached.
2. Confirming, on-device, that a human authorized an enrollment or revocation before it reaches the ENS registry.

## Decision

- **Encryption of the node's private key at rest** → `wallet-cli ring`. The key is encrypted under the ring on a machine where the Ledger *was* present at ring-creation time, then the ciphertext ships to the headless host, which decrypts without the device attached (this is the entire premise of the Ledger enrollment story — see `12_SOURCE_NOTES.md` for its verification status).
- **Signing the ENS registry transaction that grants/revokes/rotates a role** → `wallet-cli send`, which requires physical on-device confirmation for every call.

Never describe `ring` as signing anything. A Ledger judge will catch it (see `10_JUDGING.md` and Gate 3.3 in `05_BUILD_PLAN.md`, which greps for exactly this claim).

## Known limitation

The ring's domain key derives from the wallet-sync encryption key, which LKRP rotates whenever a ring member is removed. After rotation, ciphertext encrypted under the old key can no longer be decrypted. Do not build a flow that depends on decrypting old ciphertext after a membership change — re-encrypt affected key material at rotation time instead.

## Open verification

Whether `wallet-cli send` accepts arbitrary contract calldata (needed to sign a `grantRoles`/`revokeRoles` call on the ENS registry) or only native-asset transfers has not been checked yet. If it's transfer-only, this decision's second half needs a different signing mechanism. See `12_SOURCE_NOTES.md`, "Day 1 technical sanity check," item 3.
