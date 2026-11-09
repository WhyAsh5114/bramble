# Demo checklist: a private home service and a new device

One-page runbook for the finalist slot (4 minutes of demo, 3 minutes of questions). The hero path shows a new device joining a private home service with one `curl` line; a 30-second closer covers the AI agent. Exact commands, what to say, fallbacks and the sixty-second sponsor scripts live in the [operator script](DEMO_SCRIPT.md). Evidence labels: **Verified** is this author's run; **Verified (reported)** is another operator's recorded run; **Read from source**; **Open** is not yet shown.

Deadline: Saturday 7 November 2026, 09:00 IST. Cloud nodes cost testnet USDC while running. Use the one shared slot coordinated through ETH-313, and stop the nodes when you finish.

## Before the slot

- [ ] The gateway runs the ETH-319 `demo-service`: `ssh bramble-gateway 'curl -sS -H "Accept: text/html" http://127.0.0.1:9100/task | grep -c "Home server"'` prints `1`. Steps in [Deploy this build](DEMO_SCRIPT.md#deploy-this-build-before-rehearsing).
- [ ] The published release includes the local UI **Open** link. If not, use the port fallback in beat 5 of the script.
- [ ] ETH-313 releases the cloud pair to you. Nobody restarts units, edits grants or calls the forward during the take.
- [ ] Start the nodes at least 3 minutes early. Wait for `serving on` and `adopted new candidate` on both nodes. Start the worker too: it is the backup.
- [ ] The public console loads and `GET /api/requests` returns 200. A human unlocks it with the demo token. Keep the token and any `.env` off screen.
- [ ] Worker MCP uses the public console URL saved by the [Pi installation step](../agent-mcp/PI.md#steps). The [worker reachability check](DEMO_SCRIPT.md#close-with-the-agent) returns 200. The agent beat needs no worker reverse tunnel; keep the console's `bramble-console-tunnel` running.
- [ ] The new device is clean (no `~/.bramble`), has a desktop browser and is not a cloud node. Pick a fresh label, for example `home-laptop-2`.
- [ ] Remove any old `mumbai-worker` grant. Testnet balances are enough: Sepolia ETH for the admin, Hedera testnet HBAR and USDC (token `0.0.429274`) for the payer.
- [ ] Two hero-path rehearsals in the ETH-313 slot passed, with each beat timed. Otherwise present the backup path.
- [ ] A dated recording of a full run exists, and the ETH-243 Pi recording is ready for the closer.

## Beat order and timings

All hero-path windows are **Open / unverified budgets** until the ETH-313 rehearsals measure them. Switch to the backup when a deadline passes.

| Clock | Beat | Show | Switch at | Evidence at the cutoff |
| --- | --- | --- | --- | --- |
| 0:00–0:20 | 1. Private service at home | The gateway answers only on its loopback (`127.0.0.1:9100`) | n/a | Read from source |
| 0:20–0:35 | 2. A new device cannot reach it | `curl` from the new device to the gateway's public address fails | 10 s | Read from source |
| 0:35–1:05 | 3. Install with one line | `curl -fsSL https://get-bramble.whyash5114.com/install.sh \| sh`; the local UI asks to join | 60 s | Verified (reported): ETH-179, ETH-250 |
| 1:05–1:40 | 4. Operator approves | **Pending device joins** → **Approve**; then the `task` request → **Approve** | 30 s per approval | Join: Verified (reported). New device's grant: Verified (reported) in ETH-244 |
| 1:40–2:35 | 5. Opens in the browser | **Open task** on the connected screen shows the home page | 55 s | **Open**: needs ETH-313 acceptance |
| 2:35–3:30 | 6. Revoke | **Revoke access**; the page badge turns **Unreachable**; a reload fails | 60 s | **Open** for the new device; Verified (reported) for the worker |
| 3:30–4:00 | Closer: AI agent | ETH-243 Pi run: denied, request, approve, task, revoke | Recording unless rehearsed | Verified (reported): ETH-243 |

## During the take

- [ ] Say "the network refuses" for beat 2. Bramble's own denial is the gateway refusing a connection without a grant.
- [ ] The gateway decides each connection. The home server closes every connection, so each page check and reload is a fresh decision.
- [ ] Confirmation and first failed call are separate timestamps. Never say "instant revoke".
- [ ] A timeout or connection error is not a denial. Check the gateway journal for `denied for service "task"` before you call a result.
- [ ] At a deadline, say the fallback line and run the [agent path](DEMO_SCRIPT.md#fall-back-to-the-agent-path) with the identical worker `curl`. Never describe a recording as live.
- [ ] Show a payment only from this run, with its payer and time. Show HCS only if a new receipt exists. Otherwise say: "The HCS integration exists in source, but this deployment has no configured recorder."

## Questions to prepare

- **Is the home server really at home?** No. A cloud node stands in for it. Its service listens only on loopback; the gateway forward is the only way in.
- **Who decides access?** Each peer reads ENS and decides. The console writes approvals to ENS; it does not carry traffic.
- **Is Tor slow?** The Tor measurement replays a real RPC trace: median detection 7.0–11.5 s, worst 29 s, excluding chain confirmation. Live cloud calls include Tor but we have not isolated its share.
- **What do relays and STUN still see?** Peer IP addresses. WireGuard is UDP and bypasses Tor. The upgrade protects ENS reads and payment HTTP.
- **Is a public Hedera log a leak?** HCS carries salted commitments only; kind and timing are public.
- **Is there infrastructure?** Yes: relays, RPC and an administrator. No coordinator decides access.

## After the take

- [ ] Revoke the new device's `task` grant and its membership (**Members** → **Revoke**). Uninstall it with `curl -fsSL https://get-bramble.whyash5114.com/uninstall.sh | sh -s -- --yes`.
- [ ] Remove any remaining `mumbai-worker` grant and confirm the next call is denied.
- [ ] Stop the paid nodes, then check both units report `inactive` (commands in the operator script).
- [ ] Record build, times, join ID, request ID, transaction hashes, payment reference and HCS sequence (or its failure) in ETH-313.

## Submit

- [ ] Choose "Finalist and Partner Prizes". Partners: ENS, Hedera, Ethereum Foundation.
- [ ] Paste the form text from the [submission draft](13_SUBMISSION_DRAFT.md). Replace each bracket, and delete any claim marked Open that stayed Open.
- [ ] Video: 2–4 minutes, at least 720p, human narrated, not sped up. Link it in the form.
- [ ] No secrets, authenticated RPC URLs or private IPs in the repository.
