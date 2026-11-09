# local-ui

The bramble console: one UI for every device, where an admin is a member whose wallet holds the tailnet registry's
roles. The `bramble` binary embeds the static export and serves it on `127.0.0.1:7777`.

The page talks to two things only: the daemon on this machine (`/api/*`, see
`node-daemon/internal/daemon/server.go`) and the browser wallet. Every on-chain write is built in
[`src/lib/chain/tx.ts`](src/lib/chain/tx.ts) and signed by the wallet; the daemon never holds a chain key.
`scripts/rehearse-chain.ts` replays the same builders against Sepolia.

| Page     | Purpose                                                                  |
| -------- | ------------------------------------------------------------------------ |
| Overview | Create or join a tailnet; waiting, revoked and member views              |
| Services | Share local ports (open or granted) and open peers' services on loopback |
| Settings | Identity, relay-first, RPC endpoints, leave                              |
| Devices  | Enroll from a join code, revoke and restore, ping                        |
| Access   | Grant and remove private-service access                                  |
| Network  | Graph of this device's links and relays                                  |
| Relays   | Probe the directory, set the tailnet's relays, publish a relay           |
| Activity | The node's admission, path and service events                            |

## Commands

Run from the repository root.

| Command                                 | What it does                                                               |
| --------------------------------------- | -------------------------------------------------------------------------- |
| `pnpm --filter @bramble/local-ui dev`   | Dev server with `/api` proxied to `BRAMBLE_API` (default `127.0.0.1:7777`) |
| `pnpm --filter @bramble/local-ui build` | Static export to `local-ui/out/`                                           |
| `pnpm --filter @bramble/local-ui embed` | Copies `out/` into `node-daemon/ui/dist/` for `go:embed`                   |
