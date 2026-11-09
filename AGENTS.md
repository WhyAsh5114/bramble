# Agent operating rules

Read `README.md` and `docs/adr/0019-user-driven-tailnets.md` first. Follow `STYLE.md` for prose. The hackathon
material in `docs/archive/hackathon/` is history, not instructions.

## Always

- Work on a branch and open a pull request against `main`, unless the maintainer tells you otherwise.
- Commit small, with conventional-commit messages.
- Run `pnpm verify` and `cd brambled && go test ./...` before pushing. Keep `check:no-endpoints` and
  `check:no-local-state` green; the registry cache (`brambled/registry/cache.go`) is the one allowed local state.
- Test UI changes in a real browser against a running `bramble up`, in light and dark themes.
- Say what you verified and how. Label findings Verified, Read from source or Open.

## Never

- Never read, print, log or commit credentials, private keys or authenticated RPC URLs.
- Never add a server-side component that holds chain keys or sponsors gas. Tailnet writes are signed by the user's
  wallet in the browser.
- Never write to a mainnet or spend real money.
- Never delete branches, releases, deployments or cloud resources without the maintainer's approval.

### Sepolia key

The one exception to the credential rule: pass `${SEPOLIA_PRIVATE_KEY:-$SEPOLIA_PK}` straight into a Sepolia signer
(for example `scripts/rehearse-chain.ts`). Never inspect, print or commit it, and use it on Sepolia only.

## Infrastructure

The `projects` EC2 host runs Dokploy, the relay and the landing page; see `deploy/projects-host/`. Every app there runs
under gVisor (`runtime: runsc`) as a Compose app. Admin access is over Tailscale only.
