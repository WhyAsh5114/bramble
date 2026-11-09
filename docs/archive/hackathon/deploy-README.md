# How to stand up the Bramble demo on cloud machines

Goal: run the three cloud machines (gateway with `demo-service`, agent worker, relays) and the admin machine, so the demo beats run live and online (handoff section 4). This page is the runbook. For ports, files and variable names, see `REFERENCE.md`. For why the topology looks this way, see `TOPOLOGY.md`. For the two-NAT test, see `nat-lab/README.md`. To run demo beats 1, 2, 3 and 5 by hand on the running cloud nodes, see `DRYRUN.md`.

## Evidence labels

Every step ends with one label. A step is **Verified** only when someone ran it, and the note says where.

| Label            | Meaning                                                         |
| ---------------- | --------------------------------------------------------------- |
| Verified         | Ran it and saw the result (the note names the machine and date) |
| Read from source | Taken from code or docs, never run on these machines            |
| Open             | Not run. Check it before you depend on it                       |

**State on 6 Nov 2026.** Three machines exist in ap-south-1 (`gateway`, `worker`, `relays`; tailnet-only SSH, addresses in the status file `infra.json`). Steps marked "Verified (cloud, 6 Nov 2026)" ran on them. Steps still marked Open or Read from source have not run on a cloud machine; the ones that need a human-supplied secret are listed in "Blocked on secrets" below.

## What a human must supply (checkpoint H3)

Agents never look for credentials. Each item below blocks the steps that name it.

| #   | Item                                                                                                                                    | Needed by |
| --- | --------------------------------------------------------------------------------------------------------------------------------------- | --------- |
| 1   | Three small Linux cloud machines (Ubuntu 24.04 or later, 2 vCPU and 2 GB RAM each) with SSH access and a public IPv4 address            | A1        |
| 2   | The admin machine: Linux with systemd, the Ledger Key Ring file, outbound internet                                                      | C1        |
| 3   | Cloud firewall rules from `REFERENCE.md`, applied by the cloud account owner                                                            | A1        |
| 4   | Two DNS names pointing at the relays machine: one for the RPC proxy, one for the console (or a tunnel service that replaces them)       | A2, B5    |
| 5   | The dedicated Sepolia endpoint URL. It goes to the relays machine (proxy) and the admin machine only                                    | B4, C1    |
| 6   | Sepolia ETH on the operator key for gas, and the operator key `SEPOLIA_PRIVATE_KEY`                                                     | C1, D2    |
| 7   | Hedera testnet payer accounts (account ID and key) for the gateway and the worker, funded with testnet USDC `0.0.429274` and associated | E1, F1    |
| 8   | The Hedera testnet payee account ID for the relays (a public ID; no key is needed on the relays machine)                                | B1        |
| 9   | The demo token `APPROVAL_DEMO_TOKEN`                                                                                                    | C1        |
| 10  | A dedicated SSH key pair for the console tunnel (admin to relays), with the public half authorized on the relays machine                | C3        |
| 11  | A DeepSeek API key and the demo token, as two files owned by `agent` on the worker machine (mode `0600`); replaces Claude Code login    | F4        |
| 12  | Read access to the repository for the machines (public clone, or a deploy key)                                                          | A4        |
| 13  | Which tailnet and relay registry to use (names and addresses; public values)                                                            | D1        |
| 14  | Optional: a second STUN server (host and port) or a second public address on the relays machine, for the NAT type check                 | N         |

## Part A: prepare every machine

### A1. Provision the machines. Open

Create the three cloud machines and apply the firewall rules from `REFERENCE.md`. Name them `gateway`, `worker` and `relays`. Do not give the worker or gateway more open ports than the table lists.

### A2. Point DNS at the relays machine. Open

Create two A records for the names in item 4, both with the relays machine's public IPv4 address. Check with `dig +short <name>`.

### A3. Install the toolchain. Open

On each machine, install what its role needs:

- All roles: `git`, `curl`, `iproute2`, Node.js 24 or later, `pnpm` 10 (the repository pins it in `package.json`).
- gateway, worker, relays: Go 1.27 or later (`go.mod` requires it) and Bun 1.3.14. The Tor spike used Bun 1.3.14 (handoff section 9), so pin that version: `curl -fsSL https://bun.sh/install | bash -s "bun-v1.3.14"`, then link `bun` into `/usr/local/bin`.
- gateway, worker: `nftables` and `python3` (`apt install nftables python3`). The NAT box and the STUN probe need them. `install.sh` checks for both.
- relays: `caddy` (from the Caddy apt repository) and `coturn` (`apt install coturn`).
- admin: Bun, and `ssh`.

### A4. Check out the repository at a pinned commit. Verified (cloud, 6 Nov 2026)

On each machine, as root:

```sh
git clone https://github.com/WhyAsh5114/bramble.git /opt/bramble
cd /opt/bramble && git checkout <commit>
```

Use the same commit on every machine. Record the commit in the demo log. Deploy only a commit on `hackathon` that `pnpm verify` accepted. The checkout is owned by `bramble` after A5, and git refuses other users. Read it as root with the directory marked safe: `sudo git -c safe.directory=/opt/bramble -C /opt/bramble rev-parse --short HEAD`.

Run on 6 Nov 2026: all three machines at `a9db736` (merge of PR #18). The public clone worked with no deploy key.

To update a node to another commit, fetch and check it out as `bramble`, then rebuild as root with `sudo GOFLAGS=-buildvcs=false ./deploy/scripts/install.sh <role>`. Without the flag, `go build` stops with `error obtaining VCS status: exit status 128`, because root reads a checkout that `bramble` owns (Verified on `gateway` and `worker`, 6 Nov 2026, ETH-205).

### A5. Run the installer. Verified (cloud, 6 Nov 2026)

```sh
sudo /opt/bramble/deploy/scripts/install.sh <gateway|worker|relays|admin>
```

The script creates the `bramble` user and `/etc/bramble`, installs dependencies, builds the Go binaries, copies the systemd units, and seeds an empty env file with mode `0600`. It starts nothing and writes no secret. Evidence: the script passes `bash -n`; the Go builds it runs (`go build` for `brambled`, `demo-service` and `relay`) are Verified on the development machine on 6 Nov 2026. On 6 Nov 2026 it ran as root on all three machines and exited 0: it built the Go binaries, installed the dependencies and seeded the env files.

## Part B: relays machine

### B1. Fill in the relay env file. Open

Edit `/etc/bramble/relay-relay1.env` (names in `REFERENCE.md`). Set `RELAY_PAYEE_ACCOUNT_ID` to the payee from item 8. Then run `deploy/scripts/check-env.sh relay`. For a second relay, copy the file to `relay-relay2.env` with ports `:9430`, `7901`, `7905`.

### B2. Start the relay. Read from source

```sh
sudo systemctl enable --now bramble-relay@relay1
deploy/scripts/smoke.sh relays
```

Evidence: on the development machine on 6 Nov 2026, `relay -addr :19421 -meter -data-relay -payee 0.0.1 -sidecar-dir ... -sidecar-port 17891 -internal-port 17895 -price-per-byte 1 -data-host 0.0.0.0` (the unit's flags, other ports, a dummy payee) started `relay-sidecar`, and `GET /health` returned `{"ok":true}`. The unit file passed `systemd-analyze verify` apart from the binary not being installed. The unit has not run under systemd.

### B3. Tune the ephemeral UDP range. Verified (cloud, 6 Nov 2026)

The data relay opens each paid session on an ephemeral UDP port (`relay/datarelay.go` binds port 0). Install `deploy/sysctl/90-bramble-relay.conf` into `/etc/sysctl.d/` and run `sudo sysctl --system`. The firewall rule for UDP 40000-40999 then covers every session. On the relays machine, `sysctl net.ipv4.ip_local_port_range` reports `40000 40999`.

### B4. Start the logging RPC proxy. Verified (cloud, 6 Nov 2026)

Follow `deploy/rpc-proxy/README.md` (added by ETH-44, PR #2). Use the dedicated Sepolia endpoint from item 5 in `/etc/bramble/rpc-proxy.env`. Running the proxy locally against a fake upstream is Verified there; On the relays machine on 6 Nov 2026 the unit ran against the public endpoint `https://ethereum-sepolia-rpc.publicnode.com` (a human decision; no dedicated endpoint), `eth_chainId` returned `0xaa36a7`, and the call appeared in `/log`.

### B5. Start Caddy. Verified (cloud, 6 Nov 2026)

Install `deploy/caddy/Caddyfile` as `/etc/caddy/Caddyfile`, replace the two example names, and run `sudo systemctl reload caddy`. Check `curl -fsS https://<rpc-name>/healthz` returns `{"ok":true}`.

Run on 6 Nov 2026 with the names `rpc.13-233-181-104.sslip.io` and `console.13-233-181-104.sslip.io`: Caddy obtained real Let's Encrypt certificates for both on the first start, `/healthz` answered over HTTPS from outside, and a call through the public URL appeared in `/log` with the caller's real source IP (`TRUSTED_PROXY_HOPS=1` works). The console name returns 502 until the tunnel (C3) is up.

### B6. Start STUN. Verified (cloud, 6 Nov 2026)

```sh
sudo install -m 0644 /opt/bramble/deploy/stun/turnserver.conf /etc/turnserver.conf
sudo systemctl enable --now coturn
```

Set `listening-ip` and `external-ip` in the file to the machine's addresses first. The config is STUN-only (`stun-only`) and holds no credentials. Check from the gateway machine with any STUN client, or with the W8 discovery once ETH-36 merges. The nodes read the server from `BRAMBLE_STUN_SERVERS=<relays-ip>:3478`.

Run on 6 Nov 2026 with `listening-ip=<private ip>` and `external-ip=<public ip>/<private ip>`: `deploy/scripts/stun-probe.py <relays-ip>:3478 <relays-ip>:3478` from the gateway machine reported its public address (`3.110.248.161`) and "endpoint-independent mapping". With one server this checks the server answers, not the NAT type.

### B7. Register the relay in ENS (admin machine). Open

Writes a Sepolia transaction, so it needs the operator key from item 6:

```sh
cd /opt/bramble/admincli
bun run src/register-relay.ts relay1 --rendezvous <relays-ip>:9420 --sidecar-url http://<relays-ip>:7891 --price-per-byte 1
```

The usage line comes from `admincli/src/register-relay.ts`. Nodes then discover the relay from `BRAMBLE_RELAY_REGISTRY` and need no `-rendezvous` flag. Relay records are the one place endpoints may appear on chain (`docs/archive/hackathon/05_BUILD_PLAN.md` Gate 1.4), because relays are not mesh members.

## Part C: admin machine

### C1. Fill in the admin env file. Open

Edit `/etc/bramble/admin.env` (names in `REFERENCE.md`), then run `deploy/scripts/check-env.sh admin`. The admin tools read `SEPOLIA_RPC_URL` from the environment. Use the dedicated endpoint here, not the proxy, so beat 6's log shows only node traffic.

### C2. Start the approval service. Open

```sh
sudo systemctl enable --now bramble-approval
```

The service binds to `127.0.0.1:3001` and its write routes need `Authorization: Bearer <APPROVAL_DEMO_TOKEN>` (`approval/README.md` on the ETH-29 branch). Until ETH-30 merges, grant and revoke return `503 signer_not_configured`. The `approval/` package is not on `hackathon` yet, so this unit cannot run today.

### C3. Open the console tunnel. Open

On the admin machine, as `bramble`, create the SSH key from item 10 and an SSH host alias `bramble-relays` in `~/.ssh/config` pointing at the relays machine. On the relays machine, authorize the public key for a restricted user, and keep `AllowTcpForwarding` limited to that key. Then:

```sh
sudo systemctl enable --now bramble-console-tunnel
```

The tunnel makes the admin machine's `127.0.0.1:3001` appear as `127.0.0.1:3001` on the relays machine, where Caddy serves it. Only loopback is forwarded.

### C4. Check the public console URL. Open

```sh
curl -fsS https://<console-name>/api/requests
```

A read returns JSON. A write without the token must return an error status, not success: `curl -i -X POST https://<console-name>/api/requests`.

## Part D: enroll the nodes

### D1. Generate node keys on the nodes. Verified (cloud, 6 Nov 2026)

On `gateway` and `worker`, after `install.sh` built `brambled`:

```sh
sudo deploy/scripts/gen-node-key.sh /etc/bramble/gateway.env   # on gateway
sudo deploy/scripts/gen-node-key.sh /etc/bramble/worker.env    # on worker
```

The script writes the private key into the env file and prints only the public key. It refuses to overwrite an existing key. Evidence: run on the development machine on 6 Nov 2026 against a temporary env file: it wrote a key, kept mode `0600`, printed a 64-character public key, and refused a second run. Send the public key to the admin. Run on `gateway` and `worker` on 6 Nov 2026; each printed a 64-character public key.

### D2. Enroll the devices (admin machine). Open

For each node, from the admin machine:

```sh
cd /opt/bramble/admincli && bun run src/enroll.ts <label> <public-key>
```

Usage line from `admincli/src/enroll.ts`. This writes Sepolia transactions and allocates the node's mesh IP. After ETH-28 merges, the approval service performs grants itself, but enrollment stays a command.

### D3. Let the gateway trust the admin granter. Open

```sh
cd /opt/bramble/admincli && bun run src/set-acl-granters.ts <gateway-label> <granter-label>
```

Usage line from `admincli/src/set-acl-granters.ts`. The granter label is an already-enrolled identity whose key the admin machine holds.

## Part E: gateway machine

### E1. Fill in the gateway env file. Open

Edit `/etc/bramble/gateway.env`. `SEPOLIA_RPC_URL` is the proxy URL (`https://<rpc-name>/`). Set `BRAMBLE_SERVICE_NAME` to the service the console shows. Set `BRAMBLE_STUN_SERVERS=<relays-ip>:3478` after ETH-36 merges. Then run `deploy/scripts/check-env.sh gateway`. The check rejects a `SEPOLIA_RPC_URL` that looks like it carries an API key. Verified on the development machine on 6 Nov 2026: it rejected an Alchemy-style URL and accepted `https://rpc.example.com/`.

### E2. Check the TUN device. Partly verified (cloud, 6 Nov 2026)

`/dev/net/tun` exists on `gateway` and `worker` (mode `crw-rw-rw-`). Whether the restricted unit can open it is still Open. The node uses a real OS TUN (`brambled/README.md`, "Known gaps"). The unit runs as the `bramble` user with `CAP_NET_ADMIN` and `/dev/net/tun` allowed. Nobody has run `brambled` under these restrictions. If the node logs a permission error, add a drop-in that sets `User=root` and remove the `Ambient`, `CapabilityBound` and `DeviceAllow` lines. The September runs used `sudo` (Verified there, `brambled/README.md`).

### E3. Start the gateway. Partly verified (cloud, 6 Nov 2026)

```sh
sudo systemctl enable --now bramble-demo-service bramble-gateway
deploy/scripts/smoke.sh gateway
```

Cloud evidence: `bramble-demo-service` is active on the gateway machine and `GET http://127.0.0.1:9100/task` returns the JSON task. Earlier evidence: `demo-service -port 19100` started and `GET /task` returned the JSON task on the development machine on 6 Nov 2026. `brambled serve` accepted every flag the unit passes (it stopped at the missing tailnet variables, as expected). The September runbook ran the same processes by hand on an EC2 machine (`brambled/README.md`, 8 to 10 Sept 2026). The node units (`bramble-gateway`, `bramble-worker`) ran on 6 Nov 2026. The node preflight, run on each node after the rebuild, passed `env`, `sepolia-rpc`, sidecar, Tor, STUN, registry and Hedera on both; `reach:*` cannot run on a node (it needs `DEMO_*_SSH` from the host) and `relay` timed out, see below. On 6 Nov 2026 the worker never rendezvoused (ETH-205): relay discovery at start-up got a `502` (`-32602 Invalid parameters` from the load-balanced upstream) and the node then ran for its whole life with no rendezvous relay, while the gateway paid for tokens and waited. Fixed in `54e9dd7`: the sidecar and `rpc-proxy` retry the rejected `eth_getLogs` with `toBlock: latest`, `brambled` re-queries discovery when it failed, and a peer that never answers is skipped for 30 s, 60 s, then 2 min. A node needs about 40 to 60 s after `systemctl start` before it logs `serving on`, because relay discovery reads the registry over Tor; wait about 2 minutes before the first call. **Keep them stopped between runs**: with `BRAMBLE_TTL=30s` each node buys a rendezvous token about every 37 s, which cost the payer about 2.4 to 2.8 USDC per hour with both up. Start them shortly before a run and stop them after. The preflight `relay` check can time out at its 5 s limit over Tor (`/relays` took about 8 s); a direct `curl` to `127.0.0.1:7890/relays` on a node returns the relay list.

## Part F: worker machine

### F1. Fill in the worker env file. Open

Edit `/etc/bramble/worker.env`: `BRAMBLE_GATEWAY_LABEL` is the gateway's label, and `BRAMBLE_SERVICE_NAME` must match the gateway's. Run `deploy/scripts/check-env.sh worker`.

### F2. Start the worker node. Read from source

```sh
sudo systemctl enable --now bramble-worker
deploy/scripts/smoke.sh worker
```

Evidence as for E3. Every node on the tailnet must use the same `-gateway-port` (`brambled/README.md`). The units leave it at the default.

### F3. Deny check. Open

Before any grant, from the worker: `curl -sS http://127.0.0.1:8080/task`. In NAT-box mode (Part N) the forward lives inside the node's namespace, so run the call there: `sudo deploy/scripts/agent-in-box.sh curl -sS http://127.0.0.1:8080/task`. The call must fail (connection reset). The gateway log must show `denied for service`. This is demo beat 1. The September run showed this on two other machines (`brambled/README.md`, Gate 5.1).

### F4. Prepare the agent user. Open

Create a Unix user `agent` with no `sudo` and no read access to `/etc/bramble`. The agent calls only the forward on `127.0.0.1:8080`. The node's WireGuard key and Hedera payer key stay in `/etc/bramble/worker.env` (mode `0600`, owner `bramble`), so the agent never holds a backend credential.

The demo agent runs on Pi (pinned `@earendil-works/pi-coding-agent` 1.5.0) with the DeepSeek model `deepseek/deepseek-flash`, not on Claude Code (decision of the operator, 6 Nov 2026, ETH-243). Pi has native MCP support (`docs/mcp.md` in the Pi package), so it loads `agent-mcp` as is. The steps, as `agent` on the worker:

1. Copy `agent-mcp/`, `deploy/scripts/install-pi-agent.sh` and `deploy/scripts/pi-agent.sh` from the repository checkout, then run `BRAMBLE_AGENT_LABEL=<worker label> BRAMBLE_GATEWAY_LABEL=<gateway label> deploy/scripts/install-pi-agent.sh`. It installs Pi under `~/pi-runtime`, `agent-mcp` under `~/agent-mcp`, Pi's MCP config `~/.pi/agent/mcp.json` (with `BRAMBLE_FORWARD_TIMEOUT_MS=120000`, which the tools need: the `agent-mcp` default of 10 s is shorter than the 27 to 32 s gateway decision in Part G) and the skill `bramble-private-service`.
2. Put the DeepSeek key in `~/.config/bramble/deepseek.key` and the demo token in `~/.config/bramble/approval.token` (both mode `0600`, owner `agent`; item 11). The launcher reads the key and exports it as `DEEPSEEK_API_KEY` to Pi only. The MCP config reads the token with a command when it starts `agent-mcp`, so it is in no config file and not in Pi's environment.
3. The approval API listens on the admin machine's loopback. Reach it from the worker with a reverse tunnel started on the admin machine: `ssh -N -R 127.0.0.1:3001:127.0.0.1:3001 <worker>`. Or set `BRAMBLE_APPROVAL_URL` to the public console URL when you run the installer.

The launcher starts Pi with the two Bramble tools only (`--no-builtin-tools`): the agent has no shell, so it cannot read the key files. The demo prompt is one line:

```sh
~/bin/pi-agent -p "Fetch the task from the private staging service"
```

The Claude Code path in `agent-mcp/README.md` still works for a machine that has Claude Code login. Evidence: Pi 1.5.0 listed `deepseek/deepseek-flash` and answered a prompt in 1.4 s, and `pi mcp list` showed `bramble-agent` connected with `request_private_access` and `call_private_service` (Verified, worker machine, 6 Nov 2026). The launcher also appends the skill text to Pi's system prompt (`--append-system-prompt`): Pi loads a skill only when the model reads `SKILL.md`, and this agent has no read tool, so without it the model guessed the service names `staging` and `staging-api` instead of `task` (Verified, first two runs, 6 Nov 2026).

Pi access lifecycle: **Verified**, two isolated runs on 6 November 2026, 15:51–15:57 UTC. Pi first receives a denial, creates a `task` request, returns `report-private-service-health` after operator approval, and receives a denial after revoke. Full beat 3 payment/feed acceptance remains **Open** on ETH-294. Earlier overlapping runs do not count. See the measurements in [Pi run instructions](../agent-mcp/PI.md).

Verified, isolated attempt on 6 November 2026: the deployed launcher matches `91f7ca4`. The initial call reached the gateway and was denied for `task` with `no matching granted digest`. Pi created the correct access request in 33.11 s. The operator approval POST returned HTTP 401 (`unauthorized`) using `/home/agent/.config/bramble/approval.token`; no grant was approved. Both units were stopped by 15:06:39 UTC with MainPID 0, and the worker tunnel was closed. The shared-slot operator route below supersedes worker-token repair as a prerequisite. No payment or revocation timing is verified by this attempt.

Verified after the operator release, 6 November 2026 at 15:18–15:20 UTC: the worker tunnel reaches the live API on admin port 3011 and reads the same pending request. The file-derived operator header still returns HTTP 401 on a protected GET and a nonexistent POST route. No requests or grants changed. Neither node service was started; both remain inactive with MainPID 0, and the temporary tunnel is closed. The later shared-slot operator route supersedes worker-token repair as a prerequisite. See [Pi run instructions](../agent-mcp/PI.md) for the authentication check and limits.

**Launching the agent.** Without the NAT box, the agent runs as `agent` on the host and reaches the forward on the host's `127.0.0.1:8080`. With the NAT box, the node (and so the forward) sits in the `bramble-node` namespace, and a process on the host cannot reach it. Start the agent through the launcher, which enters the namespace as root and then drops to the `agent` user with no capabilities and `no_new_privs`:

```sh
sudo deploy/scripts/agent-in-box.sh ~agent/bin/pi-agent -p "Fetch the task from the private staging service"
```

The launcher also gives the agent the namespace's resolver and NAT route, so Pi reaches the DeepSeek API through the same NAT. Open: the script passes `bash -n` and its `setpriv` flags parse (Verified, development machine, 6 Nov 2026); it has not run as root or in a namespace.

## Part G: the access gate

Run these in order after parts B to F. They are the gate "agent denied, then succeeds after approval, on the cloud nodes".

1. Deny: F3. Open.
2. Request and approve through the public console, or with the admin command: `cd /opt/bramble/admincli && bun run src/set-acl.ts <agent-label> <gateway-label> <granter-label> <service>`. Open.
3. Succeed with no restart: `curl -sS http://127.0.0.1:8080/task` on the worker returns the JSON task. Open.
4. Revoke: `bun run src/revoke-acl.ts <agent-label> <gateway-label> <granter-label> <service>` or the console button. The next call fails. Open.
5. Record the measured time from the revoke transaction to the first failure. Open.

Measured on the cloud nodes with the plain node units, 6 Nov 2026 (Verified; no `bramble-node` namespace existed on the worker, one run, driven through the public console URL): deny failed with `denied for service "task"`; approval confirmed in 12 s; the first call after approval returned the task in 31 s; revoke confirmed in 11 s and the first failed call came 32 s after the revoke started. Every ENS read goes through Tor at about 1.1 s per `eth_call`, one `/device` resolve is seven calls (about 9 s), and one gateway decision resolves three records in turn, so a call takes about 27 to 32 s. Narrate that wait in the demo. Atlas accepted it (ETH-173): the gateway's decision window is 90 s, and no cache or concurrency change is planned.

Measured again on 6 Nov 2026 after ETH-205 (Verified; build `54e9dd7`, both nodes started at 10:29 UTC, rendezvous adopted on both sides by 10:30, two runs in a row through the public console API with the demo token, the same calls the console buttons make):

| Beat                                          | Run 1 (10:30 UTC) | Run 2 (10:32 UTC) |
| --------------------------------------------- | ----------------- | ----------------- |
| 1 Denied: call ends with `Connection reset`   | 83.7 s            | 15.6 s            |
| 2 Approve confirmed                           | 12.2 s            | 12.5 s            |
| 3 First call that returns the task, after 2   | 24.1 s            | 43.1 s            |
| 5 Revoke confirmed                            | 11.4 s            | 15.8 s            |
| 5 First failed call, after the revoke started | 27.5 s            | 45.6 s            |

Each call is a `curl` as the `agent` user on `worker`. A call lasts as long as the gateway decision (three ENS reads over Tor), so the values for beats 1, 3 and 5 are upper bounds with that granularity. The 83.7 s deny in run 1 was the first call after start-up; the gateway log shows `denied for service "task" (no matching granted digest)` for every denied call. Narrate the wait.

## Part H: publish the installer (relays machine)

Goal: `curl -fsSL https://<console-name>/install.sh | sh` works from any device. Caddy serves static files from `/var/lib/bramble-dist` on the relays machine. For the URLs, flags and the env-to-config table, see `docs/archive/hackathon/reference/install-host.md`.

### H1. Write the public env file. Open

On the operator's machine (not on a cloud machine), copy `deploy/env/public.env.example` to a file outside the repository and fill in the public values: the two DNS names from item 4, the tailnet name, chain id and registry addresses from item 13, and the STUN, rendezvous and data-relay endpoints. The file holds no secret. The generator reads only the names in the template, so you can also pass a node env file.

### H2. Publish a release. Verified (cloud, 6 Nov 2026, with a stub bundle)

Build the release first (`scripts/release.sh`, ETH-178; it writes `release/bramble_<os>_<arch>.tar.gz` and `release/SHA256SUMS`). Then:

```sh
deploy/scripts/publish-release.sh --release release --version <version> \
  --env <public.env> --init --caddy
```

`--init` creates `/var/lib/bramble-dist` on the first run (one `sudo`). `--caddy` installs the repository's `Caddyfile` with your two names, validates it on the machine first, keeps the old file as `/etc/caddy/Caddyfile.bak-<time>` and restores it if the reload fails. Drop both flags for later releases. `--dry-run` stages and checks without uploading.

The script:

1. checks `SHA256SUMS` against the tarballs;
2. generates `/.well-known/bramble.json`;
3. bakes the console name into `install.sh` and `uninstall.sh`;
4. refuses to upload if a staged file looks like it holds a secret;
5. uploads the tarballs and `SHA256SUMS` (replacing the previous release in `/dl`), then the node config, then `uninstall.sh`, and `install.sh` last.

### H3. Smoke-test from outside. Verified (cloud, 6 Nov 2026, with a stub bundle; re-run after the v1 path change: see the ETH-179 comments)

```sh
deploy/scripts/smoke-public.sh https://<console-name> <tailnet-name> [--run-install]
```

The script checks, over verified TLS: the `install.sh` content type and cache header, the baked host, every tarball against `SHA256SUMS`, the node config fields and a secret scan of it, 404 for unknown files, directory listings and path traversal, and that the console route still answers. With `--run-install` it also runs `curl -fsSL <base>/install.sh | sh -s -- --no-open` and then `uninstall.sh --yes`, both with a throwaway `HOME`.

## Part N: two NATs

See `nat-lab/README.md`. Short form, on the gateway and worker machines:

1. `sudo deploy/scripts/nat-box.sh up cone`. Open.
2. Install the `nat-box.conf.example` drop-in on **every unit that talks to the node over loopback**. On the gateway that is both `bramble-demo-service` and `bramble-gateway`: the gateway dials `127.0.0.1:<DEMO_SERVICE_PORT>` inside its own namespace (`brambled/gateway/server.go`), so `demo-service` must run there too. On the worker it is `bramble-worker`. Restart the units. Open.
3. Run the smoke script with `NETNS=bramble-node` so its loopback checks run inside the namespace. Open.
4. Start the agent with `deploy/scripts/agent-in-box.sh` (step F4). Open.

## Reset between runs

- RPC proxy log: `sudo systemctl restart rpc-proxy` on the relays machine. The log lives in memory only. Open.
- The chain-state reset (agent enrolled, no ACL) belongs to the reset script (ETH-70). Not written here.

## Blocked on secrets

State on 6 Nov 2026. Each item blocks the steps that name it; none is searched for by agents.

| Missing                                                                              | Blocks                                          |
| ------------------------------------------------------------------------------------ | ----------------------------------------------- |
| `RELAY_PAYEE_ACCOUNT_ID` (public Hedera testnet ID, item 8)                          | B1, B2 (relay and its ENS record B7)            |
| Hedera payer account ID and key for gateway and worker (item 7)                      | E1, E3 (`bramble-gateway`), F1, F2              |
| Tailnet name, registry addresses and deploy blocks (item 13)                         | D2, D3, E1, F1                                  |
| `APPROVAL_DEMO_TOKEN` and the Ledger Key Ring file on the admin machine (items 2, 9) | C1, C2, C3, C4 (console returns 502 until then) |
| DeepSeek key and demo token files for the `agent` user (item 11)                     | F4                                              |

## Stop the machines

When the demo ends, a human stops or deletes the cloud machines. Agents do not delete deployments.
