# Agent operating rules

Read `README.md` and `docs/adr/0019-user-driven-tailnets.md` first. Read `docs/adr/0021-paid-relays-and-audit-trail.md`
before touching payments or audit logging. Follow `STYLE.md` for prose. The hackathon material in
`docs/archive/hackathon/` is history, not instructions.

## Always

- Work on a branch and open a pull request against `post-hackathon`. `main` is frozen at the hackathon submission
  (`ee4ff53`); push to it only when the maintainer says so.
- Commit small, with conventional-commit messages.
- Run `pnpm verify` and `cd brambled && go test ./...` before pushing. Keep `check:no-endpoints` and
  `check:no-local-state` green; the registry cache (`brambled/registry/cache.go`) is the one allowed local state.
- Test UI changes in a real browser against a running `bramble up`, in light and dark themes.
- Say what you verified and how. Label findings Verified, Read from source or Open.

## Never

- Never read, print, log or commit credentials, private keys or authenticated RPC URLs.
- Never add a server-side component that holds tailnet chain keys or sponsors gas. Tailnet writes are signed by the
  user's wallet in the browser.
- Never write to a mainnet or spend real money. Hedera work runs on testnet.
- Never delete branches, releases, deployments or cloud resources without the maintainer's approval.

### Sepolia key

The first exception to the credential rule: pass `${SEPOLIA_PRIVATE_KEY:-$SEPOLIA_PK}` straight into a Sepolia signer
(for example `scripts/rehearse-chain.ts`). Never inspect, print or commit it, and use it on Sepolia only.

### Hedera testnet key

The second exception: pass `HEDERA_CLIENT_ACCOUNT_ID` and `HEDERA_CLIENT_PRIVATE_KEY` straight into the Hedera client
for live x402 and HCS runs on Hedera testnet. Never inspect, print or commit them. A node or relay may hold its own
Hedera key when its operator opts in; that key cannot write tailnet records. See
[ADR 0021](docs/adr/0021-paid-relays-and-audit-trail.md).

## Infrastructure

The `projects` EC2 host runs Dokploy, the relay and the landing page; see `deploy/projects-host/`. Every app there runs
under gVisor (`runtime: runsc`) as a Compose app. Admin access is over Tailscale only.
