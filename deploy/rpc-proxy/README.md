# How to deploy the logging RPC proxy

Goal: run `rpc-proxy` on a public HTTPS URL so the demo nodes can point at it and the console can read its log (demo beat 6). For endpoints and settings, see `rpc-proxy/README.md`.

**Status.** Running the proxy locally is Verified (see "Check it locally"). The systemd unit and Caddyfile below are Open: nobody has run them on a cloud machine. A public deployment needs human checkpoint H3 (handoff section 8).

## Before you start

- A cloud machine with Node 24 or later, `pnpm`, and this repository checked out.
- A DNS name pointing at the machine, for the public HTTPS URL.
- The dedicated Sepolia endpoint URL. A human supplies it. Do not put it in the repository, a ticket or a log.

## Steps

1. Install dependencies: `pnpm install --frozen-lockfile`.
2. Create `/etc/bramble/rpc-proxy.env`, readable only by the service user (`chmod 600`):

   ```sh
   UPSTREAM_RPC_URL=<dedicated Sepolia endpoint>
   HOST=127.0.0.1
   PORT=8545
   TRUSTED_PROXY_HOPS=1
   ```

   `TRUSTED_PROXY_HOPS=1` tells the proxy that one reverse proxy (Caddy, below) sits in front. Without it, every call logs as `127.0.0.1`.

3. Install the service unit `deploy/rpc-proxy/rpc-proxy.service` into `/etc/systemd/system/`. Edit `WorkingDirectory` and `User` to match the machine. Then run `systemctl daemon-reload && systemctl enable --now rpc-proxy`.
4. Install Caddy and use `deploy/rpc-proxy/Caddyfile`, replacing `rpc.example.com` with the DNS name. Caddy obtains the TLS certificate. By default it replaces any client-supplied `X-Forwarded-For` with the client address (Open: check this against the Caddy version you install).
5. Point nodes at `https://<your-name>/` as their Sepolia RPC URL.
6. Point the console split screen at `https://<your-name>/log`. Poll it with `?since=<lastSeq>`.

## Run the tests

Requires Node 24 or later. The tests start a fake upstream on a local port and never contact a real RPC provider.

```sh
pnpm --filter @bramble/rpc-proxy test
```

## Check it locally

Use a fake upstream so no real endpoint is needed.

1. In one terminal, start any HTTP server that answers JSON-RPC on port 18999.
2. In another: `UPSTREAM_RPC_URL=http://127.0.0.1:18999/ PORT=18545 pnpm --filter @bramble/rpc-proxy start`.
3. Send a call: `curl -s -X POST localhost:18545/ -H 'content-type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}'`.
4. Read the log: `curl -s localhost:18545/log`. The entry shows `sourceIp`, `method` and `params`.

## Reset the log between demo runs

Restart the service: `systemctl restart rpc-proxy`. The log lives only in memory, so a restart clears it. The proxy has no reset endpoint, because a write path would need a secret.

## What is public

Anyone who can reach `/log` sees the source IP, method and parameters of each accepted call (not rate-limited or oversized requests; see `rpc-proxy/README.md`), including other people's calls to the public proxy. This is the point of beat 6. Do not send anything sensitive through the proxy.
