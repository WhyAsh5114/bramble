# ADR 0004 — Admin CLI writes directly to the registry; three distinct EAC roles for enroll/rotate/revoke

**Status:** Decided and verified, Sept 7 2026. Gate 2.1's table-driven permit/deny test passed end-to-end on the live hackathon Sepolia deployment (`admincli/test/gate2.1-eac-check.ts`).

## Context

ADR 0002 decided that all ENS interaction goes through a per-node sidecar, and proposed that sidecar's future API would include `POST /admin/enroll` and `POST /admin/revoke`. Phase 2 (`05_BUILD_PLAN.md` Gate 2.1) needed to actually build enrollment, revocation, and key rotation — and building it surfaced that the sidecar was the wrong place for it.

`03_ARCHITECTURE.md`'s component diagram (drawn after ADR 0002) already shows this differently: a separate **Admin CLI**, using `wallet-cli send`/`wallet-cli ring`, writing straight to the registry — not routed through any node's sidecar. That diagram is correct and this ADR formalizes it, amending ADR 0002's proposed API rather than silently diverging from it.

**Why the sidecar was the wrong place:** the sidecar is deliberately per-node and read-only — `sidecar/src/ens/config.ts`'s own comment states "Section A scope is resolution, not enrollment/revocation." Admin operations are keyed to an *operator's* wallet (a Ledger, eventually — Phase 3), completely independent of any one device's identity. Routing enroll/revoke through a specific node's sidecar would tie an operator action to whichever machine happens to be running that sidecar, for no reason. `04_TECH_STACK.md` already anticipated a standalone admin CLI package.

## Decision

A new workspace package, `admincli/` (Node+TS, Bun runtime, viem — same stack as `scripts/provision-dev-tailnet`), writes directly to the ENSv2 registry. No sidecar involvement.

### Three EAC roles, not one

Gate 2.1 requires enrollment, revocation, and key rotation to be governed by **distinct** EAC roles. The obvious implementation — revoke by clearing the `pubkey` text record (what `scripts/provision-dev-tailnet/set-pubkey.ts` already does for Gate 1.3) — collides with rotation, since both are writes to the same `pubkey` key, and EAC's `grantSetterRoles` scopes by function + record key, not by the value written. Two record keys are needed, not one:

| Action | Mechanism | Role |
|---|---|---|
| Enroll | `register()` on the tailnet's subregistry | `ROLE_REGISTRAR`, root-scoped on the subregistry (see below) |
| Rotate | `setText(name, "pubkey", newValue)` on the device's resolver | `ROLE_SET_TEXT` scoped to the `pubkey` setter, via `grantSetterRoles` |
| Revoke | `setText(name, "revoked", "true")` on the device's resolver | `ROLE_SET_TEXT` scoped to the `revoked` setter, via `grantSetterRoles` |

`revoked` was chosen over reusing the key `status` (which Gate 0.3's own proof-of-concept used as an example) specifically to avoid colliding with `DeviceRecord.status` — the registry's own token-status enum from `getState()`, an unrelated field. This is **additive**: the existing clear-`pubkey` revocation path stays exactly as it was for Gate 1.3's already-verified 3.7s measurement; `admission/loop.go`'s authorization rule just gained one more clause (`AND !record.Revoked`).

### `ROLE_REGISTRAR` is not `REGISTRATION_ROLE_BITMAP`

This was the one open unknown going into Section A: does `register()` check the *caller's* role, separately from the `roleBitmap` parameter (which only governs what the *new token's owner* can subsequently do)? Verified from primary source rather than assumed or taken from the docs page (same discipline as the `UserRegistryImpl.initialize` signature drift in `11_SOURCE_NOTES.md` — the ENSv2 docs page for this deployment doesn't spell out the exact mechanism either, though it does confirm `ROLE_REGISTRAR` by name): `ensdomains/contracts-v2`'s `PermissionedRegistry.sol`, `_register()`, for a fresh (never-registered) label:

```solidity
if (checkRoles) {
    _checkRoles(ROOT_RESOURCE, RegistryRolesLib.ROLE_REGISTRAR, msg.sender);
}
```

`RegistryRolesLib.sol` defines `ROLE_REGISTRAR = 1 << 0` (root-scoped only), granted via `grantRootRoles()` (`EnhancedAccessControl.sol`), gated by holding `ROLE_REGISTRAR_ADMIN`. This is a completely separate grant target from `REGISTRATION_ROLE_BITMAP` (already in `sidecar/src/ens/config.ts`), which is the *outcome* of a successful `register()` call — the rights handed to the new device subname's owner — not the gate on who may call `register()` in the first place. Both constants now live in `config.ts`, clearly distinguished.

### Bonus finding: a role-read function exists

While verifying the above, found that `roles(anyId, account) view returns (uint256)` is a real, documented function (`EnhancedAccessControl.sol`, overridden in `PermissionedRegistry.sol`), confirmed against the ENSv2 docs' "Enhanced Access Control" page too. This corrects `config.ts`'s prior comment claiming no verified read ABI existed. Not needed for Gate 2.1 (the `simulateContract` permit/deny pattern, proven by Gate 0.3, is sufficient and is what the test uses), but now available and added to `registryAbi`.

## Consequence

- `05_BUILD_PLAN.md` Phase 2's Gate 2.1 is verified — see its entry for the tx hashes and pass/fail table.
- `admincli`'s write paths are scanned by `scripts/check-no-endpoints.mjs` (Gate 1.4) like every other ENS write path in this repo.
- Phase 2 Sections B (ACL schema) and C (gateway enforcement) build on this: ACL grants will need their own EAC role, following the same `grantSetterRoles`-scoped-to-a-key pattern established here.
- Ledger integration (Phase 3) replaces `admincli`'s software `SEPOLIA_PRIVATE_KEY` signing with `wallet-cli send` device-confirmed signing — the registry calls themselves don't change, only how the transaction gets signed.
