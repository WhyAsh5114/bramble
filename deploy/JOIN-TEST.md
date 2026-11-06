# Test a second-device join on Zephyrus

Use this how-to for ETH-250, demo beats 2 (request and approve) and 3 (connect).
Run from the repository root on Zephyrus. The harness needs Python 3, `curl`,
`sh` and the installer's archive/checksum tools. The installed device needs no
Go, Bun, Node.js or pnpm.

## 1. Prepare an isolated run

Read the latest release comment in ETH-244 before using the public host. Ask
Infra-Claude to coordinate the join, approval, service grant and cleanup in
ETH-255. Do not SSH to cloud nodes, restart them or change their configuration.
Infra-Claude owns cloud operations. Stop if `bramble0` exists: the current
uninstaller removes that interface even for a userspace install.

The script creates a new HOME at `<run-dir>/home`, with Bramble under
`<run-dir>/home/.bramble`. It rejects an existing run directory. It passes an
explicit environment with no inherited credentials, browser settings or proxy
settings. It uses no root, system service or shell profile. Evidence outside
HOME remains after uninstall; do not upload the device directory or its key.

## 2. Dry-run a local release before publication

Use a release directory with `bramble_linux_amd64.tar.gz` and `SHA256SUMS`.
The archive must contain the real `node-daemon` executable as `bramble`, the
`brambled` helper and compiled `bramble-sidecar`. Build and embed the local UI
before compiling `bramble`.

For a native Linux amd64 test bundle, use these commands in your own worktree:

```sh
pnpm install --frozen-lockfile
pnpm --filter @bramble/local-ui build
pnpm --filter @bramble/local-ui embed
mkdir -p release/local-bin
(cd node-daemon && go build -trimpath -ldflags='-X main.version=eth-250-local' -o ../release/local-bin/bramble .)
(cd brambled && go build -trimpath -o ../release/local-bin/brambled .)
bun build --compile --minify sidecar/src/index.ts --outfile release/local-bin/bramble-sidecar
tar -C release/local-bin -czf release/bramble_linux_amd64.tar.gz bramble brambled bramble-sidecar
(cd release && sha256sum bramble_linux_amd64.tar.gz > SHA256SUMS)
```

These commands prepare test input. They do not publish a release. At commit
`5724e2f`, `scripts/release.sh` still builds the older `brambled` CLI as
`bramble`, without the launcher, helper or embedded UI. Do not use that bundle
to claim a working onboarding flow.

Run the default installer first, with a new directory for each attempt:

```sh
python3 deploy/scripts/join-test.py local \
  --release-dir release \
  --run-dir /home/anonjr/bramble-work/join-test/eth-250-local-default-1
```

The harness serves the release and a pending-only join fixture on an ephemeral
loopback port. The fixture uses the real tailnet name and registry address,
but never approves a request, writes to Ethereum Name Service (ENS), contacts
cloud nodes or simulates a connected result. The real launcher generates the
device identity and serves its API.

If the default installer fails at startup, reproduce the install-only path:

```sh
python3 deploy/scripts/join-test.py local \
  --release-dir release \
  --run-dir /home/anonjr/bramble-work/join-test/eth-250-local-manual-1 \
  --manual-start
```

`--manual-start` runs `curl | sh -s -- --no-start`, then runs the installed
`bramble up -d -open=false`. It is a diagnostic workaround. It does not prove
that the default installer works.

Expect `setup`, then a unique `zephyrus-…` device label, `pending`, a join ID,
and a public fingerprint. `bramble status` must report `pending` for
`bramble-dev-c91e55e9.eth`. Before approval, `/api/test` must return
`ok: false` with “This device is not connected yet.” The harness then uninstalls
the device and stops the local server, including when a local run fails.
Approval and real mesh connectivity remain **Open** in this dry run.

## 3. Run against the real published installer

Wait for Infra-Claude's release confirmation in ETH-244 and the coordinated
slot in ETH-255. The script refuses the current stub, the wrong tailnet,
registry, chain ID or join origin before creating HOME. Confirm the real
release includes the launcher, both helpers and the embedded local UI.

```sh
python3 deploy/scripts/join-test.py start \
  --base https://console.13-233-181-104.sslip.io \
  --run-dir /home/anonjr/bramble-work/join-test/eth-250-public-1
python3 deploy/scripts/join-test.py request \
  --run-dir /home/anonjr/bramble-work/join-test/eth-250-public-1
```

`start` exercises the public `curl | sh --no-open` path. It leaves the local
UI running and prints its actual loopback URL; port 7777 is preferred, and
the launcher tries the next available port if needed. The `request` command
names the device and submits the real join request through the local API.
It prints the unique label, join ID and fingerprint for the approver. If the
join submission fails while status is `ready` or `error` without a join ID,
rerun `request` with the same run directory; the script keeps the existing identity. Once a join ID exists,
the script refuses a duplicate submission.
If you use `request`, do not also click **Request to join** in the browser.

Keep the second device free of admin credentials. Infra-Claude or the human
uses the existing admin console session to find **that request**, compare
its label and fingerprint, and approve it. Do not retrieve or print a console
token. Approval enrolls the device; it does not grant access to a service.
Ask Infra-Claude to coordinate a grant for the **first entry** in the public
config's `services`, and to confirm that the cloud peers discover the device.
The harness sends an empty test body, so the daemon selects that first target.
Record its `peer` and `service` before requesting the grant. An empty `services`
list cannot pass this service test.

After the decision, run status again:

```sh
python3 deploy/scripts/join-test.py status \
  --run-dir /home/anonjr/bramble-work/join-test/eth-250-public-1
```

Expect `pending` → `approved` → `connected`. A connected status must include
a mesh IP and a peer. A **Relayed** path is valid for this test; a **Direct**
badge requires separate observed evidence. Allow for Tor bootstrap and ENS
reads: `approved` alone is not a pass. If a problem appears, record the public
error and next step, then coordinate with Infra-Claude before changing nodes.

Run the service test after the connected status and grant:

```sh
python3 deploy/scripts/join-test.py test \
  --run-dir /home/anonjr/bramble-work/join-test/eth-250-public-1
```

The harness allows 110 seconds for the daemon's 95-second service test. A pass
requires `ok: true` and a target HTTP status from 200 through 299. An HTTP 200
from the local `/api/test` endpoint can carry `ok: false`; that is a failure.
Record the target peer, service, `ms`, mesh IP and path from the saved JSON.

## 4. Uninstall and finish the evidence

Ask Infra-Claude or the human approver to revoke **only this test enrollment**
and remove its service grant through the existing console. Confirm cleanup
before discarding the join ID. Local uninstall does not revoke ENS enrollment.

```sh
python3 deploy/scripts/join-test.py uninstall \
  --run-dir /home/anonjr/bramble-work/join-test/eth-250-public-1
```

Expect the device process to stop and `<run-dir>/home/.bramble` to disappear.
Keep `install.log`, `run.json`, `config.json`, `first-run.json`, `pending.json`,
`state.json`, `test.json` and `uninstall.json` as the public test record. The
script never reads the identity file. Report failures with the command,
timestamp, release commit/version and the last successful step.

## 5. Give the human the command and expected screens

After the public flow passes, ask the human to run this on their own device:

```sh
curl -fsSL https://console.13-233-181-104.sslip.io/install.sh | sh
```

**Open:** this default command is not yet verified against a real published
release. Do not present it as working until the public rerun passes.

**Read from source:** the terminal prints the platform, install directory,
tailnet, checksum result and local UI address. The installer opens the browser
unless the device has no graphical session. The expected screens are:

1. **Name this device**: a device-name field, **Continue**, and a fingerprint.
2. **Join a network**: enter `bramble-dev-c91e55e9.eth`, then **Request to join**.
3. **Waiting for approval**: the network, device name and request ID; it updates
   automatically. Compare the fingerprint with the admin.
4. **You are in. Connecting…**: the approved device gets a private address.
5. **You are on the network**: mesh IP, devices and path badges. After a service
   grant, **Test connection** reports the request result and elapsed time.

Do not promise these browser screens from a placeholder UI or CLI-only bundle.
For the human's normal HOME, uninstall with:

```sh
curl -fsSL https://console.13-233-181-104.sslip.io/uninstall.sh | sh -s -- --yes
```

## 6. Check the recorded result before claiming completion

**Verified, 6 November 2026, 12:38 UTC, Zephyrus, Linux amd64, commit `5724e2f`:**
the locally built real launcher, `brambled`, compiled sidecar and embedded UI
pass checksum verification and extraction. The default installer fails after
launch: it invokes `bramble` without `up`, and the CLI prints usage. With
`--manual-start`, the real CLI/API reaches `setup` and `pending`, status reports
the expected tailnet, the pre-approval connectivity test returns `ok: false`,
and uninstall removes the device directory and stops the daemon. The sidecar
and mesh are not exercised while the join remains pending.

**Verified, 6 November 2026, 12:38 UTC, read-only public config check:** the host
still advertises version `0.0.0-stub`, tailnet `smoke-test.eth` and placeholder
registry `0x0000000000000000000000000000000000000001`.

**Open:** real release publication, default installer startup, live approval,
service grant, connected status, successful mesh request, browser observations
on the public flow, and enrollment/grant cleanup. ETH-244 owns publication;
ETH-255 carries the coordination request. Request Tor-Claude's review of the PR
into `hackathon`; this local dry run does not close those public-flow steps.
