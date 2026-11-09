# How to present Bramble: a private home service and a new device

The hero path tells one story: a private service runs at home behind NAT, a new device installs Bramble with one `curl` line, the operator approves it, and the service opens in the device's browser. Revoke ends it. A 30-second closer says the same flow works for an AI agent. The agent and worker path from the earlier script stays as the [backup](#fall-back-to-the-agent-path); switch to it when the new-device path misses a deadline. This is the ETH-319 operator script for handoff section 4.

**Evidence cutoff: 6 November 2026, 18:30 UTC.** The hero path needs the ETH-319 build (the home page in `demo-service` and the **Open** link in the local UI). At the cutoff that build is not deployed and the new-device path has not run end to end. Every hero-path timing below is an **Open / unverified budget**. Two rehearsals in the ETH-313 slot must replace them with measurements before the presentation.

## Choose what you can show

Evidence labels: **Verified** means this author ran it. **Verified (reported)** means the named issue records a real run by another operator. **Read from source** means code supports it but nobody ran it end to end. **Open** means acceptance is outstanding. Do not describe another operator's run as your own.

| Segment | Evidence at the cutoff | Stage choice |
| --- | --- | --- |
| Home page from the private service (`demo-service`) | Verified, loopback only: browsers get the page at `/` and `/task`; `curl`, `demo-agent` and the MCP tool still get the JSON task. Headless Chromium rendered it in light and dark; it loads without subresources and waits 10 s between `/task` checks. Not yet on the gateway | Deploy it, then check it once through the forward |
| **Open** link in the local UI | Verified with the mock daemon (Playwright, 1280 and 390 px). Read from source for the real daemon: a Go test with a fake node returns `services[].url` | Needs a release that includes ETH-319 |
| Curl-install and enrollment | Verified (reported): ETH-179 and ETH-250 install and enroll a non-cloud Linux amd64 device at `970461c` | Show live |
| New device reaches the service | **Open**: ETH-244 failed at `970461c` with no WireGuard handshake. The handshake fix (PR #59) and the rendezvous payment URL (PR #63) are merged; ETH-313 owns live acceptance | Show live only after an ETH-313 rehearsal passes. Otherwise use the backup |
| Gateway denial, grant and revoke for the cloud worker | Verified (reported): ETH-244 POSTFIX-A and B, two passes without restart | Backup path |
| Worker lifecycle on the official ENSv2 tailnet | Verified (operator session, 6 November 2026, 20:47–20:52 UTC): the gateway denied the worker, approval wrote the grant to `bramble-mumbai-20261107.eth` in 15 s, the call returned HTTP 200 with the task after 44 s, revoke confirmed in 19 s and the next call was refused | Use for the agent closer. The fresh-device browser path is still Open |
| Pi agent: denied, request, approve, task, revoke, denied | Verified (reported): ETH-243, two isolated runs on 6 November 2026, 15:51–15:57 UTC | Closer, as a labeled recording unless rehearsed live |
| x402 USDC settlement | Verified (reported): ETH-204 journals hold paid rendezvous transactions and HashScan URLs | Show a settlement with its time and payer. Approval does not cause a payment |
| New Hedera Consensus Service (HCS) record | **Open**: ETH-244 reports Hedera activity `not_configured` | Claim it only when the console shows a new, mirror-verified receipt |
| Direct path (beat 4) and provider view (beat 6) | **Open** for the live run | Show separately only with rehearsed evidence; keep their acceptance gaps explicit |

## Deploy this build before rehearsing

The slot owner (ETH-313) runs these after the ETH-319 pull request merges. They are **Read from source**; nobody has run them for this build.

1. On the gateway, check out the merge commit and rebuild only the private service. It keeps the name `task`, port `9100` and loopback-only binding. The checkout at `/opt/bramble` is owned by `bramble` and pinned to a commit (`deploy/README.md`, step A); replace `<commit>` with the ETH-319 merge commit:

   ```sh
   ssh bramble-gateway 'cd /opt/bramble && sudo -u bramble git fetch origin hackathon && sudo -u bramble git checkout <commit>'
   ssh bramble-gateway 'cd /opt/bramble/brambled && sudo -u bramble env HOME=/var/lib/bramble go build -o bin/demo-service ./cmd/demo-service && sudo systemctl restart bramble-demo-service'
   ssh bramble-gateway 'curl -sS -H "Accept: text/html" http://127.0.0.1:9100/task | grep -c "Home server"; curl -sS http://127.0.0.1:9100/task'
   ```

   Expect `1`, then the JSON task with `"status":"healthy"`. This rebuilds `demo-service` only; record that the gateway's `brambled` binary still comes from the earlier commit. If `go` is not on the `bramble` user's path, build as root the way `deploy/scripts/install.sh gateway` does.

2. Publish a release that includes the local UI change, with the existing `deploy/scripts/publish-release.sh` flow. A device installed from the `970461c` release has no **Open** link. If no new release ships, use the port fallback under beat 5 in [Commands for each beat](#commands-for-each-beat).
3. Confirm the served node config still lists the service and the rendezvous payment URL:

   ```sh
   curl -fsS https://get-bramble.whyash5114.com/.well-known/bramble.json
   ```

   Expect `"services": [{"peer": "mumbai-gateway", "service": "task", "path": "/task"}]` and a `rendezvousPaymentUrl`. Both were present at 18:10 UTC.

## Prepare before the audience arrives

The commands below are **Read from source / unverified by this author**. Run them on the operator workstation with the existing SSH aliases. Do not run them during another operator's slot. Keep a browser and one terminal visible. The latest recorded gateway and worker state is stopped (16:10 UTC). Obtain a new operator slot before starting them. Prepare a recording with its date, build and evidence limits; the recording's existence is **Open** until the operator checks it.

- [ ] Reserve exclusive use of the cloud pair and `mumbai-worker` grants. Agree that nobody restarts units, changes grants or calls the forward during the take. ETH-244 discarded an overlapping pass.
- [ ] Confirm the deployed build and open the public console at `https://get-bramble.whyash5114.com`. Confirm `GET /api/requests` returns 200. On the workstation, check `systemctl --user is-active bramble-approval bramble-console-tunnel`. On relays, check `systemctl is-active bramble-relay@relay1 rpc-proxy caddy coturn` through SSH.
- [ ] Have the human enter the existing demo token in the console's password field and click **Unlock**, before recording. Reloading locks the console. Keep secret files, headers, shell tracing and agent configuration off screen. Do not give the operator token to an agent to perform this script.
- [ ] Remove any previous grant through the existing operator revoke-by-label flow for `mumbai-worker`, even if no console card exists. Requests live in process memory; a restart can hide the card while ENS keeps the grant. HTTP 409 `not_granted` means no grant to remove. Verify the next denied call against a new gateway journal line.
- [ ] Check current Sepolia ETH, Hedera testnet HBAR and USDC balances through the existing operator balance check. Historical balances are not current funds. Testnet USDC is token `0.0.429274`; the reported payer is `0.0.11334961`. Confirm the actual payer mapping before attributing spend to a worker.
- [ ] Plan for roughly 2.5 USDC/hour in ETH-244 and up to about 4 USDC/hour in the ETH-204 retry-loop observation, both with two nodes up. Nine minutes (three-minute warmup plus six-minute presentation) is about 0.38–0.60 testnet USDC at those rates; fifteen minutes is about 0.63–1.00. Budget the rehearsals separately: 17 minutes of node uptime, as in the two ETH-205 runs, costs an estimated 0.71–1.13 testnet USDC at 2.5–4 USDC/hour. Add the presentation and any extra warmup time. ETH-244 also reports a burst of token purchases at startup. These estimates are not caps and exclude HCS fees. Check the configured per-payment cap against the sidecar; the displayed demo budget is advisory and does not stop payments.
- [ ] Check HCS readiness before promising it: a real topic, payer configuration and a fresh mirror-verified record must exist. If missing, report the missing names `HEDERA_AUDIT_TOPIC_ID`, `HEDERA_CLIENT_ACCOUNT_ID`, `HEDERA_CLIENT_PRIVATE_KEY` to the operator; never inspect their values. Use the disclosure line below until resolved.
- [ ] Rehearse twice before the presentation slot. Record build, warmup, start/end times, request ID, approve/revoke hashes, each curl result, payment reference, and HCS sequence/consensus time or its failure. After each run, check the final grant is gone. Two prior cloud runs do not verify your spoken timing or current deployment.


### Start the nodes at least three minutes early

Reserve time for a full off-stage lifecycle before T−3:00 when possible. The later pass follows a full lifecycle; three minutes and one denied call do not establish the same conditions. Start the nodes no later than T−3:00, after the slot owner releases the machines:

```sh
ssh bramble-gateway 'sudo systemctl start bramble-demo-service bramble-gateway'
ssh bramble-worker 'sudo systemctl start bramble-worker'
```

Wait for current-start `serving on` and peer `adopted new candidate` messages on both nodes. Use the unit start times to bound any journal view. `pnpm demo:preflight` is available with role/connection settings in the [launcher how-to](../scripts/demo/README.md); investigate any failure. The five-second checks probe direct TCP connectivity to rendezvous relays and HTTP `/health` on payment sidecars. They do not measure a Tor read. Investigate a failed probe before using the paid path; the separate `/relays` query uses `CURL_TIMEOUT_SECONDS`.

Once ready, call beat 1 once off stage to warm the denied path:

```sh
ssh bramble-worker "sudo -u agent curl -sS -m 150 -w '\nHTTP %{http_code} after %{time_total}s\n' http://127.0.0.1:8080/task"
```

Expect a reset and HTTP 000, with a matching new gateway `denied for service "task" (no matching granted digest)` line. A timeout or reset without that denial line is not proof of enforcement. If readiness and the warmup do not finish before the slot, delay the start or use the labeled recording. Three minutes is a preparation target, not a cold-start guarantee. The post-fix first denial takes 121 s after startup.

When time allows, continue off stage through approve, successful call, revoke and a fresh denied call, using the operator actions below. Record each duration and remove the rehearsal grant. If you only warm beat 1, warn that beat 3 may still run cold: one denial does not establish the next call duration. Use the six-minute script or labeled recording unless your own full rehearsal fits the short schedule.

### Also for the hero path

- [ ] Use a clean device: Linux or macOS with a desktop browser, no `~/.bramble` directory, not a cloud node. If it held an earlier install, run `curl -fsSL https://get-bramble.whyash5114.com/uninstall.sh | sh -s -- --yes`.
- [ ] Choose a fresh lowercase device label, for example `home-laptop-2`. Never reuse a revoked label such as `eth244-postfix-device`.
- [ ] Keep three windows ready: the new device's terminal and browser, the console at `https://get-bramble.whyash5114.com` (unlocked by a human), and an operator terminal off screen.
- [ ] Rehearse the hero path twice in the ETH-313 slot. Record each beat's duration, the join ID, the grant and revoke hashes, and when the page first loaded and first showed **Unreachable**. Replace the budgets below with these numbers.

## Present the home-cloud story

About 3 minutes 30 seconds, then the 30-second closer. Start each action as soon as the previous result appears. The **Switch at** column is a stage deadline counted from the start of that action. At a deadline, say the fallback line and move to the [agent path](#fall-back-to-the-agent-path). All windows are **Open / unverified budgets**.

| Clock | Beat | Do | Say (one sentence) | Switch at |
| --- | --- | --- | --- | --- |
| 0:00–0:20 | 1. A private service at home | Show the gateway terminal running the home check below. | "This machine stands in for a home server behind NAT; its service answers only on its own loopback, with no public port." | n/a |
| 0:20–0:35 | 2. A new device cannot reach it | On the new device, run the reachability check below. | "From a new laptop there is nothing to connect to: no address, no open port, no VPN account." | 10 s |
| 0:35–1:05 | 3. Install with one line | Run the installer. The local UI opens at `http://127.0.0.1:7777`. Enter the device label, keep the prefilled tailnet `bramble-mumbai-20261107.eth`, click to join. | "One line installs Bramble, and the device asks to join our tailnet." | 60 s to a pending request |
| 1:05–1:40 | 4. The operator approves | In the console, under **Pending device joins**, check the label and key and click **Approve**. Off screen, create the `task` request for the label (command below), then click **Approve** on its card. | "I approve this device, and then its access to the home service; the console writes both to ENS, and each peer reads ENS itself, so no coordinator decides access." | 30 s per approval to a confirmed transaction |
| 1:40–2:35 | 5. The service opens in the browser | When the local UI shows **Connected**, click **Open task** under **Private services**. Keep the tab visible while it loads. | "The browser on the new device opens the home server through Bramble's encrypted tunnel; nothing is exposed to the internet." | 55 s to the page |
| 2:35–3:30 | 6. Revoke, and access dies | On the request card, click **Revoke access** and note the clock. Wait for the page's badge to turn **Unreachable**, then reload to show the browser's connection error. | "I revoke it; the gateway refuses the next connection, and the page loses access on its next check." | 60 s from the click to **Unreachable** |
| 3:30–4:00 | Closer: the same for an AI agent | Play the labeled ETH-243 Pi recording, or run the agent live only if rehearsed. | "The same approval and revoke work for an AI agent, which gets the service and never a credential." | Recording only if live is not rehearsed |

### Commands for each beat

Beat 1, gateway terminal:

```sh
ssh bramble-gateway 'ss -ltn "sport = :9100"; curl -sS http://127.0.0.1:9100/task'
```

Expect one listener on `127.0.0.1:9100` and the JSON task.

Beat 2: on the operator workstation, read the gateway's public address from its existing SSH config. Copy that public address privately to the new device; a clean device has no SSH alias:

```sh
# Operator workstation
ssh -G bramble-gateway | awk '$1 == "hostname" {print $2}'

# New device: replace <gateway-public-address> with that output.
curl -sS -m 5 "http://<gateway-public-address>:9100/task"
```

Expect `Connection refused` or a timeout after 5 s. Say it plainly: this is the network refusing, not yet a Bramble decision. Bramble's own denial is the gateway refusing a member without a grant, shown in beat 6 and in the backup path.

Beat 3, new device:

```sh
curl -fsSL https://get-bramble.whyash5114.com/install.sh | sh
```

The installer waits up to 20 s for the local UI and opens it with `xdg-open` or `open`. If no browser opens, open the printed address, normally `http://127.0.0.1:7777`.

Beat 4, operator terminal off screen. Keep the token in the environment and the header off screen:

```sh
curl -sS -X POST https://get-bramble.whyash5114.com/api/requests \
  -H "Authorization: Bearer $APPROVAL_DEMO_TOKEN" -H 'Content-Type: application/json' \
  -d '{"agentLabel":"home-laptop-2","gatewayLabel":"mumbai-gateway","service":"task","note":"home page for a new device"}'
```

Expect HTTP 201 and `pending`. Replace `home-laptop-2` with the chosen label. Create it after the join is approved: a grant for a label that has no name yet is **Open**. To save stage time, approve the join and the request back to back; the device can show **Approved** before **Connected**.

Beat 5, new device: the **Open task** link points to `http://127.0.0.1:<port>/task`, the device's loopback forward. The page shows **Reachable**, the gateway host name and the time it was served. If the release has no **Open** link, list the forward's port on the device with `ss -ltnp | grep brambled` (Linux) or `lsof -nP -iTCP -sTCP:LISTEN | grep brambled` (macOS), and open `http://127.0.0.1:<port>/task` for each `127.0.0.1` port in turn. The local UI is `7777`.

Beat 6: every response from the home server closes its connection, so the page's next check and every reload ask the gateway again. A call already in flight can finish after the revoke. A connection error after revoke is evidence only with a matching gateway line:

```sh
ssh bramble-gateway 'sudo journalctl -u bramble-gateway --since "-2 min" --no-pager -o cat | grep "denied for service \"task\""'
```

The **Unreachable** badge reports a failed health check, including a timeout or transport error. It does not prove revocation without that gateway denial line. The browser waits up to 90 s for a health check and then waits 10 s before checking again; the 60 s stage budget may expire first.

After the take, under **Members**, click **Revoke** for the device label to end its membership too.

## Close with the agent

Use the [Pi run instructions](../agent-mcp/PI.md). Per the operator update on 6 November 2026 at 18:44 UTC, worker MCP uses `https://get-bramble.whyash5114.com` for the approval API. Set `BRAMBLE_APPROVAL_URL` to that URL when installing Pi; the installer saves it in the MCP configuration. The agent beat needs no worker reverse tunnel. The private service still uses the worker's `http://127.0.0.1:8080` Bramble forward.

Before the slot, check the public route from the worker:

```sh
ssh bramble-worker 'curl -sS -m 10 -o /dev/null -w "console HTTP %{http_code}\n" https://get-bramble.whyash5114.com/api/requests'
```

Expect `console HTTP 200`. This checks reachability only; it does not prove authenticated request creation or private service access. The public route is **operator-reported**; this author has not rehearsed the Pi lifecycle with it. The ETH-243 measurements below used the earlier temporary worker tunnel. The workstation's `bramble-console-tunnel` still serves the public console and remains in preflight.

The live prompt, as `agent` on the worker:

```sh
~/bin/pi-agent -p "Fetch the task from the private staging service"
```

A full live agent lifecycle takes minutes: ETH-243 measured 32.14 s and 32.31 s for the post-revoke call alone. For the 30-second closer, show the ETH-243 recording with its date and say it is recorded. Run it live only as the backup path below.

## Fall back to the agent path

Switch here when a hero-path deadline passes, the new device stays **Approved** without **Connected**, or the page never loads. Say: "The new-device path is not answering in time, so I will show the same lifecycle with our agent worker, which passed twice today." Use [Close with the agent](#close-with-the-agent) for Pi setup through the public console, and the [recording fallback](demo/fallback.md) if that URL is unavailable. The worker sequence below runs beats 1 (denied), 2 (request and approve), 3 (works and pays) and 5 (revoke) with the identical worker `curl`.

### Budget for the measured waits

Use the newest cloud timings when selecting a script. These are individual observations, not guarantees or a percentile distribution.

| Measurement                                              | ETH-244 POSTFIX-A | ETH-244 POSTFIX-B |
| -------------------------------------------------------- | ----------------- | ----------------- |
| Beat 1: denied call                                      | 121.0 s           | 26.4 s            |
| Beat 2: approval confirmed                               | 19.5 s            | 17.0 s            |
| Beat 3: first successful call after approve              | 40.9 s            | 39.1 s            |
| Beat 5: revoke transaction confirmed                     | 16.9 s            | 20.2 s            |
| Beat 5: first failed call, measured from starting revoke | 46.1 s            | 50.8 s            |

Read from recorded evidence: both passes use `970461c`, begin at 15:38:09 and 15:41:57 UTC, and run without a node restart between them. Services started at 15:32:36–15:32:38 UTC. The first denial includes transport startup delay; the cause of rendezvous timeouts is not isolated. These call-completion bounds include ENS reads over Tor. They do not isolate Tor overhead or warm decision latency. The cloud peers use a direct endpoint, which does not prove separate-NAT traversal or relay-only public-device access.

Do not add revoke confirmation to revoke-started-to-denial: the latter already includes it. One denial warmup does not establish a warm successful call. The 121-second first denial does not fit either short stage window below. Even the later revoke observation exceeds the three-minute script's 50-second window. Keep those schedules as unverified budgets, and switch to labeled evidence at their deadlines.

The separate [Tor measurement](measurements/2026-11-06-revocation-over-tor.md) replays a real RPC trace on one machine. Its 7.0–11.5 s median range and 29 s worst observation exclude transaction confirmation. Do not substitute them for the live cloud revoke figures. Sources, exact request IDs and cleanup are in the [acceptance log](measurements/2026-11-06-onboarding-acceptance.md) and [ETH-244 post-fix report](/ETH/issues/ETH-244#document-validation-postfix).


### Set up the operator actions

Use this exact same worker curl for beats 1, 3 and 5. Do not change the endpoint or restart nodes between them. The command runs curl as Unix user `agent`; it does not demonstrate an AI agent session.

For beat 2, the operator submits one pending request through the existing authenticated request flow, with this JSON body. In the three-minute script, create it off stage after warmup and leave it pending; inspect and approve it on stage. In the six-minute script, create it on stage:

```json
{
  "agentLabel": "mumbai-worker",
  "gatewayLabel": "mumbai-gateway",
  "service": "task",
  "note": "live access lifecycle"
}
```

The route is `POST /api/requests`. Keep its Authorization header off screen. Expect HTTP 201 and `pending`, then select this request in the console and click **Approve** once. See the [API reference](reference/approval-api.md) for the request schema. No duration in this body means “Until revoked.” If you choose a duration in rehearsal, disclose that it uses a server-side revoke timer that needs the approval process to keep running. The card's price field currently says “Estimate unavailable”; do not invent a quote.

For beat 3, keep a journal payment URL ready from this run, with its timestamp, and open `/payments` if the deployed payer mapping works. Label an earlier payment as earlier. For beat 5, note the clock when clicking **Revoke access**, wait for confirmation, then repeat the curl and verify a new gateway denial. The console transaction link is Sepolia Etherscan; payment and HCS links are HashScan.

### Present the backup in three minutes

Use this schedule only after a full rehearsal fits it, with the request already pending. The times include the waits and operator actions. Start the next action as soon as its preceding result appears. The four action windows allow 35 / 30 / 55 / 50 seconds; the latest first denial takes 121 s and one revoke takes 50.8 s, exceeding their windows. This schedule has not passed a timed rehearsal. Show payment evidence during the successful-call wait. If a window ends with the result still pending, switch to labeled evidence; keep the closing five seconds even if an action started late.

| Clock     | Operator action                                                                                                                                           | Say                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| --------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0:00–0:05 | Show worker terminal and console.                                                                                                                         | “Bramble gives a worker scoped access to a private service. The gateway checks authorization from ENS.”                                                                                                                                                                                                                                                                                                                                  |
| 0:05–0:40 | Run the warmed curl. Show the reset and matching denial.                                                                                                  | “This worker is enrolled, but it has no grant for `task`. The gateway reads ENS over Tor before deciding.” When denial arrives: “Denied. The private task stays behind the gateway.”                                                                                                                                                                                                                                                     |
| 0:40–1:10 | Inspect the pending request's identity, gateway and `task`; click Approve once. Show the Sepolia receipt.                                                 | “The worker requested access before we started. I approve it now for this service, until I revoke it. The grant is written on Sepolia.”                                                                                                                                                                                                                                                                                                  |
| 1:10–2:05 | Repeat the same curl without restarting anything. Keep its pending state visible while opening the prepared settlement; then show HTTP 200 and task JSON. | “Same worker, same call; the gateway reads the changed authorization through Tor. While it checks, this is a real relay settlement with its payer and time. x402 charges testnet USDC for rendezvous tokens or data allotments; approval is not a payment trigger.” If HCS is ready, point to the new receipt; otherwise: “The new HCS audit record is not configured here yet.” On success: “The private service now returns its task.” |
| 2:05–2:55 | Click Revoke access; time from click. After confirmation, repeat curl and show the fresh denial.                                                          | “Now I remove the grant. Confirmation and the next observed denial are separate timestamps.” On denial: “That same call is refused again. The peer enforces the changed authorization.”                                                                                                                                                                                                                                                  |
| 2:55–3:00 | Keep the denied result visible, or label the fallback if still pending.                                                                                   | “Scoped, revocable access, enforced by peers from ENS.”                                                                                                                                                                                                                                                                                                                                                                                  |

### Present the backup in six minutes

Use the same prepared actions and evidence gates. Do not start a second lifecycle just to fill time. If an action needs more time than its slot, shorten the later explanation and optional installer segment. Use the elapsed-action deadlines below and switch to labeled evidence if needed.

| Clock     | Operator action                                                                                                        | Say                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| --------- | ---------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0:00–0:15 | Point to worker, private service and console.                                                                          | “A worker needs the private task API. The operator can grant a service and later remove it. We will use the identical call throughout.”                                                                                                                                                                                                                                                                                                                                                              |
| 0:15–0:45 | Beat 1: curl, then fresh denial evidence.                                                                              | “An enrolled identity is not permission to every service. The gateway finds no matching granted digest for `task`.” Narrate the Tor wait before the result.                                                                                                                                                                                                                                                                                                                                          |
| 0:45–1:20 | Beat 2: create request, inspect card, approve and show receipt.                                                        | “This is `mumbai-worker`, asking `mumbai-gateway` for `task`. ENSv2 permissioned registries and per-name resolvers supply identity and authorization. The console writes the grant; the peers make their own access checks. The service name shown here becomes a digest in the access record.”                                                                                                                                                                                                      |
| 1:20–2:10 | Beat 3: same curl, no restart, show HTTP 200 and task JSON.                                                            | “The next decision reads the changed authorization. We did not replace the worker's request.” Use wait narration. On success: “This is the private service's task response.”                                                                                                                                                                                                                                                                                                                         |
| 2:10–3:05 | Show current-run settlement, payer/time and HashScan. Show HCS evidence only if ready.                                 | “x402 pays for relay service in Hedera testnet USDC. Connection attempts buy rendezvous tokens; data relay uses explicit allotments. A service approval does not initiate payment, and retries can cost tokens even when access is denied.” If HCS works: “HCS records an ordered salted commitment for grant, revoke or payment. Kind and timing stay public. The retained event and salt let the console check the commitment against the mirror.” Otherwise state the missing-configuration line. |
| 3:05–3:50 | Beat 5: click revoke, show receipt, repeat curl, verify denial.                                                        | “Watch the same call after revoke. We measure from starting revoke to the first failed call, and keep the confirmation time separately.” On denial: “The grant no longer authorizes this service.”                                                                                                                                                                                                                                                                                                   |
| 3:50–4:40 | Keep the real results visible; explain privacy scope.                                                                  | “The sidecar sends ENS reads and payment HTTP through separate Tor isolation keys. The RPC provider still sees names, records and timing. The payment service still learns the payer account. WireGuard, STUN and data relays use UDP outside Tor; peers and relays can see IP addresses. We protect these control-plane requests, not every network observation.”                                                                                                                                   |
| 4:40–5:30 | If timed to fit, show the new device install and join screen ([beat 3 commands](#commands-for-each-beat)). Otherwise revisit the grant and revoke receipts. | Installer: “A new device installs Bramble with one line and asks to join; in this run the service calls use the agent worker.” Without it: “A request card is not enforcement. These confirmed chain changes and repeated calls are the evidence.” |
| 5:30–6:00 | Show denied result, real links and disclosed gaps.                                                                     | “Bramble uses ENSv2 for authorization, Tor for these control-plane requests, and Hedera for paid relays and the optional audit trail. There is infrastructure and an administrator, but no trusted coordinator deciding access for the peers. These are the results and remaining gaps of this deployment.”                                                                                                                                                                                          |


### Narrate the authorization waits

Speak these lines while a call is pending. Stop when the result arrives. The two latest successes take 40.9 and 39.1 s; the first denial takes 121 s. Never promise a 30-second ceiling.

| Elapsed | Narration                                                                                                                                                            |
| ------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0–10 s  | “The gateway reads authorization from ENS through Tor. We leave the command visible while that check completes.”                                                     |
| 10–20 s | “The approval service writes chain state. Each peer reads that state and decides what it accepts.”                                                                   |
| 20–30 s | “The latest second-pass denial took 26.4 seconds; the first took 121. Successful calls took about 39 to 41 seconds. These are observations from today's cloud runs.” |
| 30–45 s | “This call is still pending. I will call it allowed or denied only when the result and gateway evidence agree.”                                                      |

Do not attribute the whole delay to Tor alone: chain/RPC behavior, sequential reads and the transport are included. Do not call a timeout a successful revoke.


## Use a sixty-second sponsor version

These are **unverified spoken budgets**. At the measured latencies, a full live lifecycle does not fit sixty seconds. Use one rehearsed live result with previously captured, clearly dated evidence for the other steps, or narrate over a labeled recording. Never describe a recorded result as live.

### ENS: authorization and revocation

- **0–10 s:** “Bramble uses ENSv2 as the authorization source for a private WireGuard mesh. A name identifies a device and its resolver supplies the records peers check.”
- **10–25 s:** Show request and confirmed grant. “Permissioned registries, per-name resolvers and Enhanced Access Control separate enrollment, rotation, revocation and access rights. The access record holds a digest of the service.”
- **25–45 s:** Show successful call followed by revoke receipt and failed call, labeled live or recorded. “The same call works after the grant and fails after revoke, without restarting the nodes. Each peer makes its own decision from ENS.”
- **45–60 s:** “The console writes authorization; it does not centrally enforce traffic. Today's cloud runs observed the first failed call 46.1 and 50.8 seconds after revoke began. ENS is part of access control, beyond naming.”

### Ethereum Foundation: Tor scope and ENSv2

- **0–15 s:** “Bramble combines ENSv2 authorization with a Tor control plane. Peers read names and authorization themselves, so a relay cannot grant membership.”
- **15–30 s:** Show the grant and repeated call. “The sidecar routes ENS reads and payment HTTP through Tor with separate isolation keys. We do not publish private member endpoints in ENS.”
- **30–45 s:** “Cloud calls, including ENS reads over Tor, took 26.4 or 121 seconds to deny and about 39 to 41 seconds to succeed. We have not isolated Tor’s share. The first failed call came 46.1 or 50.8 seconds after revoke started.”
- **45–60 s:** “The provider still sees queried records and timing. WireGuard and STUN stay outside Tor, and relays see peer addresses. The claim is control-plane protection and no trusted coordinator. A live before/after provider-log demonstration remains unverified here.”

### Hedera: paid relay service and HCS

- **0–15 s:** Show a real settlement with its time and payer. “Bramble uses x402 to buy relay service with Hedera testnet USDC. Rendezvous tokens and data allotments make the relay a paid service.”
- **15–30 s:** Open its HashScan settlement link. “This is a settled transfer. Relay connection attempts trigger payments; clicking Approve does not. The payer mapping determines attribution.”
- **30–45 s:** If accepted, open the fresh HCS HashScan entry. “The new HCS trail records salted commitments for grants, revokes and payments. The console checks a retained opening against a mirror message; event kind and timing remain public.” If missing, say: “The HCS integration exists in source, but this deployment has no configured recorder. I cannot show a new submitted audit record.”
- **45–60 s:** “Payment settlement and audit commitments are separate. HCS does not enforce access or prove log completeness. ENS supplies authorization; peers enforce it. HashScan lets us inspect the real Hedera evidence we have.”


## Recover when a beat stalls

Use these stage deadlines as **unverified presentation budgets**, not protocol timeouts. Count deny/success from starting curl, approval from clicking Approve, and revoke-to-failure from clicking Revoke access (including confirmation and the follow-up curl). The short script also stops each action at its clock-window end if that comes first; these deadlines do not extend the three-minute take. At the deadline, leave the pending state visible and switch to the prepared evidence. Keep the 150-second curl cap for an off-stage diagnosis. Do not issue repeated approve/revoke writes to hurry a pending transaction.

| Symptom / stage deadline                                                  | Say                                                                                                              | Do                                                                                                                                                                                                                                         |
| ------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| New device shows **Approved** but not **Connected** after 60 s | “This device is approved, but its tunnel is not up yet, so I will show the same lifecycle with our agent worker.” | Switch to the backup path. Leave the device as it is and diagnose after the take. |
| Home page not loaded 55 s after **Open task**, or a connection error before revoke | “The home server has not answered this device yet.” | Check the gateway journal. `denied for service "task"` means the grant has not reached the gateway yet; anything else is transport. Switch to the backup at the deadline. |
| The tab shows JSON instead of the home page | “This is the same private service in its raw form.” | The gateway still runs the old `demo-service`. Continue, and redeploy after the take. |
| No **Open task** link on the connected screen | “I will open the forward by its local address.” | The device runs an older release. Use the port fallback in beat 5. |
| Nodes not ready by T0                                                     | “The live nodes are still starting. This recording shows the tested build and its date.”                         | Use the labeled recording. Diagnose off stage with current-start readiness/adoption logs. Do not start another take against a warming path.                                                                                                |
| Deny has no result after 35 s, or success has no result after 55 s        | “This check is still pending. Today's runs vary, so I will show the earlier measured result with its timestamp.” | Stop narrating a live outcome. Switch to recording. A timeout, tunnel dial failure or missing adoption is not an access denial. Check fresh gateway logs off stage.                                                                        |
| Initial call succeeds before approval                                     | “This worker still has access from an earlier run. We need to reset before showing denial.”                      | Stop the sequence. The operator removes the existing grant by label, waits for propagation and verifies a fresh denial before restarting. An empty console queue does not prove an empty ACL.                                              |
| Approval still pending after 30 s, HTTP 202, or `write_unresolved`        | “The transaction receipt is not confirmed. I will not claim the grant is active yet.”                            | Inspect the request and its real Sepolia hash; let the service reconcile. Do not create another request or duplicate the write. Continue with labeled evidence.                                                                            |
| Success still denied after approval                                       | “The grant is confirmed, but this check has not observed access yet.”                                            | Verify identity, service `task`, receipt and fresh gateway decision. Retry the call off stage within a bounded two-minute investigation; do not approve again. If still denied, report the failure and stop the paid nodes.                |
| Revoke has no failed-call evidence after 50 s, or a call succeeds         | “Revocation is not yet demonstrated by this call. The confirmation and enforcement timestamps differ.”           | Inspect receipt and fresh denial. A call already in flight can succeed. Recheck off stage; do not say “instant revoke” or count a timeout as denial.                                                                                       |
| Console 401 or disabled buttons                                           | “The operator console is locked.”                                                                                | Have the human unlock privately. Do not paste tokens into the shared terminal or agent session.                                                                                                                                            |
| Console unavailable or signer error                                       | “The approval path is unavailable. I will show the recorded transaction and call sequence.”                      | Check the user units/tunnel off stage. Do not restart the approval service blindly: requests and revoke timers are in memory.                                                                                                              |
| Payment feed stale, empty or HashScan slow                                | “Settlement evidence is delayed or unavailable in this view.”                                                    | Use a real earlier transaction, explicitly labeled with its time, or state the gap. Do not report zero spend from a failed feed. Never substitute an HCS link for a payment.                                                               |
| HCS `Post failed`, `Awaiting mirror`, no receipt or missing configuration | “The service-access result is separate. This HCS record has not been verified.”                                  | Show the actual state. Request missing configuration from the operator off stage. Use `/audit/fixtures` only if visibly labeled synthetic. Never call `Commitment matched` proof that the source event occurred or every event was logged. |

## End the take

Save the timing and evidence notes, including every skipped or failed segment. The operator revokes any remaining grant and checks that no service access remains. For the hero path, also revoke the new device under **Members** and uninstall it with `curl -fsSL https://get-bramble.whyash5114.com/uninstall.sh | sh -s -- --yes`. Stop the paid nodes when the exclusive slot ends:

```sh
ssh bramble-worker 'sudo systemctl stop bramble-worker'
ssh bramble-gateway 'sudo systemctl stop bramble-gateway'
ssh bramble-worker 'systemctl is-active bramble-worker'
ssh bramble-gateway 'systemctl is-active bramble-gateway'
```

Expect `inactive` on both. An inactive `is-active` check normally returns a nonzero exit code. Check the payer feed after mirror indexing to confirm spending no longer increases. The unit enablement can restart spend after a machine reboot; have the operator account for that between presentations. Do not leave nodes running while editing slides.

For technical backing, use the [payment feed reference](17_AGENT_PAYMENTS_REFERENCE.md), [HCS record format](16_HCS_AUDIT_RECORD_FORMAT.md), [current HCS UI](../approval/src/components/hcs-panel.tsx), [audit configuration](../approval/src/lib/audit-runtime.ts), [request card](../approval/src/components/request-card.tsx), [README privacy scope](../README.md#crops) and [demo launcher](../scripts/demo/README.md). Older documents can retain planned status; current code supports capability claims, while the cited cloud comments establish live acceptance.
