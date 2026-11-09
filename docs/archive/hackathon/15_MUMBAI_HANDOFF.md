# Bramble at ETHGlobal Mumbai: brief update and agent handoff

As of 6 November 2026. Deadline: **Saturday 7 November 2026, 09:00 IST**. Late submissions are not accepted.

This file extends Bramble's own Mumbai brief, `docs/14_MUMBAI_BUILD_BRIEF.md`, with the new work that wins prizes. Read that brief first. Where this file and the brief disagree, this file wins.

Evidence labels:

| Label | Meaning | Trust |
| --- | --- | --- |
| Verified | Ran it and saw the result | Build on it |
| Read from source | Taken from code or docs, never run end to end | Build on it, test it first |
| Open | Not tested | Check it before depending on it |

---

## 1. Decision and targets

We enter Bramble on the **continuity track**. Do not reopen this decision without a human checkpoint.

| Prize | Amount | Why we fit | Status |
| --- | --- | --- | --- |
| Hedera: Built on Hedera (Continuity Track) | Up to 2 teams, $750 each | x402 relay payments already settle on Hedera through Blocky402. Needs one new Hedera service (section 5.2) | Target |
| EF: Best CROPS Upgrade to an Existing Project | $1,500, single award | Adds a structural privacy property: the control plane goes over Tor (section 5.1) | Target |
| Finalist | Some slots are reserved for continuity projects; confirm with staff | The access lifecycle is a strong live demo | Target |
| ENS: ENSv2 in an existing project | $3,000 | Requires ENSv2 on mainnet, which still resolves through ENSv1 | Skip unless an ENS judge explicitly says yes |
| Others | | Would be bolted on | Skip |

**What would change the decision:** the team cannot register on the continuity track; the Hedera judge says the planned new work does not count; the Tor upgrade proves unworkable on the cloud nodes. Raise any of these at once as a human checkpoint.

---

## 2. Event rules every agent must follow

- **Continuity.** "Clearly document pre-existing work and include new features or functionality developed during the hackathon."
  - The baseline is commit `28e2a8f`, tagged `pre-mumbai`. Everything after it is new.
  - The README needs a "Pre-existing vs new" section. Add it early and keep it current.
  - The EF and Hedera continuity prizes judge **only the new work**. Polish and bug fixes do not count as new work.
- **Commit history.** Small, frequent, meaningful commits with conventional-commit messages. No single large commit and no end-of-day batching: "Submissions with large single commits or missing histories may be disqualified."
- **AI usage.** AI may assist but must not build the whole project.
  - Commit all spec files, prompts and planning artifacts to the repo.
  - Humans make and approve the design decisions: each ADR names its human approver.
  - Keep `docs/AI_USAGE.md` current, including which paths agents wrote and which account they commit as.
- **Video.** One video for all judges. Aim for 2:30–2:55, at least 720p, no speed-up, no AI voiceover or text-to-speech, no phone recording.
- **Finalist session.** 4 minutes of demo and 3 minutes of questions. Judged on technicality, originality, practicality, usability and WOW factor. At submission, choose "Finalist and Partner Prizes".
- **One EF prize per project.**
- **Secrets.** Never commit, print or log keys, private keys or RPC URLs that carry credentials.

---

## 3. Invariants

1. **No central enforcement.** Peers check admission and access rules themselves; the approval service only writes chain state that peers read independently.
2. **No topology on-chain** for mesh members (`pnpm check:no-endpoints`).
3. **No local-state writes** in `brambled/**/*.go` (non-test) or `sidecar/src/**` (`pnpm check:no-local-state`).
4. **`dashboard/` stays read-only.**
5. **Access records are digests**, never plaintext service names, hosts or ports (ADR 0005).
6. **The agent never holds a backend credential.**
7. **`pnpm verify` passes before every push.**
8. **Honest claims.** The claim is "no *trusted* coordinator", not "no infrastructure". The access-rule write is software-signed, and so are enrollment transactions now that Ledger signing is out of scope. State every property the new work changes accurately in the README.

---

## 4. The demo

Every task traces to a beat. A task that changes nothing on screen, and makes no on-screen claim true, is cut. Build it to work live and online:

- **Live:** the nodes (gateway, worker, relay) run on cloud machines, never on venue wifi. The presenter's laptop shows a browser and one terminal.
- **Online:** the approval console has a public URL.
- **Recorded fallback:** a full recording exists before T-6.

| Beat | Time | What the viewer sees | Work |
| --- | --- | --- | --- |
| 0. Framing | 0:00–0:10 | "AI agents need your private services, not your credentials." | none |
| 1. Denied | 0:10–0:25 | A real Claude Code agent tries to fetch a task from a private staging service and fails: "not granted" | W1, W2 |
| 2. Request and approve | 0:25–0:50 | A request card shows the agent's ENS name, the service, the duration and a price estimate. A human approves, and a transaction link appears | W1 |
| 3. Works and pays | 0:50–1:20 | The same call succeeds with no restart. The console shows the Hedera USDC payment (HashScan link) and the new Hedera service's record | W3, W10 |
| 4. Direct path | 1:20–1:40 | The two nodes sit behind different NATs. The console shows STUN discovery, a hole-punched direct path, and a fallback to the relay when the direct path is blocked | W8 |
| 5. Revoke | 1:40–2:00 | One click on Revoke, and the agent's next call fails. The feed shows the cut-off and the measured time | W1, W4 |
| 6. What the provider sees | 2:00–2:35 | Split screen: the RPC provider's log before the upgrade (node IPs, network name, a poll every 5 s) and after it (only rotating Tor exits) | W9, W11 |
| 7. Close | 2:35–2:50 | "Scoped. Revocable. Paid. Private. Enforced by every peer from ENS." | none |

The WOW moment is beat 5, revocation live. Rehearse it until it is reliable.

**Questions to prepare** (from `docs/09_JUDGING.md`, plus these):

- "Doesn't Tor make it slow?" Give the measured revocation time over Tor.
- "What can relays and STUN servers still see?" WireGuard traffic is UDP and cannot go through Tor. Relays see peer IP addresses, and a STUN server sees the address of whoever asks. The upgrade protects the control plane: ENS reads and payments.
- "Isn't a public Hedera log a privacy leak?" Only salted hashes are posted.

---

## 5. New work

The original brief's workstreams W1 to W6 stay as written: the approval console, the agent with its MCP tools, payments in the flow, the activity feed, the launcher and public deployment, and the docs. **Ledger signing (W7) is out of scope.** W8 (STUN and hole punching) is **required**, and W9–W11 are added.

### 5.1 W9: control plane over Tor (EF CROPS Upgrade, the "P")

**The leak.** Every node's sidecar polls ENS every 5 s through a public Sepolia RPC endpoint. The provider sees each node's IP address, which network registry it reads, and how often it polls. Payment requests leak the same way.

**The change.** All of the sidecar's outbound HTTP goes through Tor using tor-js 0.4.4, with one isolation key per purpose: `ens` and `pay`. The relay operator and the RPC provider then cannot link a node's reads to its payments.

| Path | Today | Change |
| --- | --- | --- |
| `sidecar/src/ens/client.ts` | `createPublicClient({ chain: hackathonSepolia, transport: http(RPC_URL) })` | `http(RPC_URL, { fetchFn: torFetch('ens'), timeout: 60_000 })` |
| `sidecar/src/payments/client.ts` and `datarelay.ts` | `wrapFetchWithPaymentFromConfig(fetch, …)` | Wrap `torFetch('pay')` |
| new `sidecar/src/tor/` | none | One `TorClient` per sidecar, readiness state, `torFetch(key)` with a retry under a fresh key, a status endpoint |

**Constraints and measurements:**

- **Verified (5 Nov, Bun 1.3.14, tor-js 0.4.4).** Runs under Bun with `storage.MemoryStorage`, so `check:no-local-state` stays green. Ready after 51 s with no snapshot; the first request took 21 s, later ones 4–5 s. About 1 in 10 first requests failed and succeeded when retried under a new isolation key.
- **Startup.** The sidecar must not serve ENS results until Tor is ready. Report "starting Tor" through the status API, and keep the last-known peer set while waiting.
- **Faster cold start (Open).** tor-js accepts a directory snapshot as `bootstrap`. Fetch it into memory at startup from a host we control. Measure the effect in Bun.
- **Hedera signer (Open).** `@x402/hedera`'s `createClientHederaSigner` may make network calls outside the wrapped fetch. The first W9 task checks this. Anything that cannot go over Tor goes in the README's CROPS "P" line.
- **Revocation latency.** Re-measure with the poll over Tor (estimate 5–10 s against 3.7 s today) and publish the number.
- **README CROPS section** (EF requires it), one or two lines per letter: C (what keeps working if our relay or RPC provider disappears), O (licence and buildability), P (what each third party learns), S (how keys are held; node, payer and enrollment keys are software keys today).
- **Unchanged:** `brambled` makes no direct RPC calls. Check that, and route any found through the sidecar.

**Done when** the logging proxy (W11) shows only Tor exit IPs for a node's registry reads and payments, `pnpm verify` is green, and the measured revocation time is in the README.

### 5.2 W10: a new Hedera service (Hedera continuity)

A human picks one option with the Hedera judge (checkpoint H1). Showing existing payments in the console is polish and does not count.

| Option | What it adds | Demo moment |
| --- | --- | --- |
| A. HCS audit trail (default if the judge gives no preference) | Each grant, revoke and payment posts a salted hash commitment to an HCS topic. The console verifies entries through the mirror node | Beat 3 shows the topic entry with a HashScan link |
| B. Scheduled Transactions | Relay usage accumulates and settles in one scheduled transaction per period | Beat 3 shows the scheduled settlement executing |
| C. HCS-14 agent identity | The agent gets an on-chain identity on Hedera, linked to its ENS name | The request card shows the agent's Hedera identity |

Post hashes only. Plaintext would publish the network's membership. **Done when** the chosen service runs on Hedera testnet in the demo flow, its record appears in the console with a HashScan link, and the README lists it as new work.

### 5.3 W11: demo infrastructure for live and online judging

- **Cloud topology.** Three small cloud machines: gateway with `demo-service`, the agent's worker node, and the relays. The admin and approval service runs on the human-controlled admin machine. Use dedicated Sepolia and Hedera endpoints; the public Sepolia pool lags.
- **Logging RPC proxy.** A small public HTTPS JSON-RPC proxy logs source IP, method and parameters, then forwards. Before W9 a node points straight at it; after W9 the same node points at it through Tor. The console shows both logs side by side. It holds no secrets and logs to memory only.
- **Public approval console** at a stable URL, behind a shared demo token for writes.
- **`pnpm demo` launcher, reset script and preflight.** The preflight checks that Tor is ready on every node and that STUN discovery works.
- **Recorded fallback** of the full flow, made by T-6.

### 5.4 W8: STUN and hole punching (required)

**Today.** Relay use is selected explicitly. Nodes do not discover their public endpoint, and a blocked direct path is not detected automatically.

**The change.**

1. Each node discovers its public UDP endpoint with STUN (RFC 5389 binding requests), from the same socket WireGuard uses.
2. Peers exchange discovered endpoints through the existing **paid rendezvous** channel, never on-chain (invariant 2).
3. Both sides send simultaneous UDP probes to open NAT mappings, then WireGuard handshakes over the punched path.
4. If no handshake completes within a timeout, the node falls back to the relay automatically and keeps retrying the direct path in the background.

**Constraints:**

- No local-state writes in `brambled` (invariant 3).
- STUN uses UDP and cannot go through Tor. The STUN server learns the asker's address. State this on the README's CROPS "P" line.
- Peer admission still comes only from ENS; an endpoint learned from rendezvous never grants access (invariant 1).
- Test in two stages: unit and rootless integration tests with a simulated NAT, then two real cloud machines behind different NATs or a NAT gateway.

**Done when** two nodes behind separate NATs establish a direct WireGuard path using STUN-discovered endpoints with no manual `-peer` endpoint flag, a blocked direct path falls back to the relay automatically, and `pnpm verify` and `pnpm test:access-flow` stay green.

---

## 6. Schedule

Hours count back from the deadline (T). Each phase ends at a gate that must pass on the cloud topology.

| By | Phase | Gate |
| --- | --- | --- |
| Now | Baseline checks, "Pre-existing vs new" README section, agents running | Checks green |
| T-30 | W1 request API and console shell; W2 MCP agent; W9 Tor client; W8 STUN discovery and endpoint exchange; W11 cloud nodes | The agent is denied, then succeeds after approval, on the cloud nodes; STUN discovers a public endpoint |
| T-22 | W3 payments in the flow; W10 Hedera service; W8 hole punch with relay fallback; W11 logging proxy | Beat 3 shows a HashScan settlement and the new Hedera record; a direct path forms across NATs |
| T-16 | W9 complete with measurements; W4 activity feed; W5 launcher and reset | Beat 6's before and after works; revocation over Tor is measured |
| T-10 | Full run of all beats, twice in a row after a reset | Two clean runs |
| T-6 | Feature freeze; recorded fallback made | A human confirms the freeze |
| T-4 | README, ADRs, submission text | Human review |
| T-2 | Final video recorded by a human (human voice) | Video meets section 2 |
| T-1 | Submit, choosing "Finalist and Partner Prizes" with Hedera and EF selected | Submitted |

**Cut order if behind:** the beat-6 split screen (show the logs one after the other); W4 polish; the snapshot bootstrap; the 4-minute extras. **Never cut W8, W9, W10, or beats 1 to 5.**

---

## 7. Roles

One human operates the agent system. The rest of the team tests, improves and gives feedback locally on their own machines, and pushes small commits to feature branches. Only a human merges to main.

**Agents.** Claude Sonnet 5.5 engineers do the bulk implementation. Two GPT-6-Astra engineers (Network and Tor lanes) implement the hardest workstreams. GPT-6.1-Sol agents (Core, Hedera, Infra UI, QA) review, test, drive the browser and write UI. Opus 5.5 and GPT-6-Astra act as principals for plans, interfaces and architecture, and are called only when a task touches signing, keys, access-rule cryptography or the Tor client's core, crosses two or more lanes, or has failed review twice. A third failure goes to a human.

**Cross-review.** Every task with more than about 20 changed lines is reviewed by an agent from the other model family, who runs `pnpm verify` and the task's own checks.

**Lanes and paths.**

| Lane | Owns |
| --- | --- |
| Core and console | `approval/`, `agent-mcp/`, `admincli/` refactor, W1–W4 |
| Network | `brambled/` STUN, hole punching and relay fallback (W8) |
| Tor (CROPS) | `sidecar/src/tor/`, sidecar transport, CROPS README section (W9) |
| Hedera | The new Hedera service and its console panel (W10) |
| Demo and infra | `deploy/`, logging proxy, launcher, reset, preflight, video (W11) |

**UI testing.** A Sol tester that did not build the segment runs agent-browser against the **public** console URL, posts screenshots at 1280 px and 390 px with the beat, commit hash and verdict, and waits at least 60 s for the Tor-backed sidecar. A human approves each UI segment.

---

## 8. Human checkpoints

Agents stop and ask only at these points, and batch their questions.

| ID | When | What a human decides or provides |
| --- | --- | --- |
| H0 | Start | Confirm continuity registration and the team; ask ETHGlobal staff whether continuity finalist slots exist and how finalists are judged |
| H1 | Before W10 starts | Ask the Hedera judge which option in section 5.2 counts. If no answer by T-28, use option A |
| H2 | Early | Ask the EF contact what "zk-https" meant (likely zkTLS) |
| H3 | When first needed | Secrets and money: the dedicated Sepolia endpoint, the Hedera testnet payer account, cloud machines, the domain or tunnel, the demo token |
| H4 | Each UI segment | Approve or reject screenshots |
| H5 | Any change to this file's decisions | Stack, beats, targets, cut order |
| H6 | T-6 | Freeze and cuts |
| H7 | T-2 to T-1 | Record the video; submit |

**Always ask first** before anything irreversible or public: spending real money, writing to a mainnet, publishing somewhere new, deleting a branch, repo or deployment, or force-pushing (never allowed). Agents never look for credentials on any machine; a missing secret blocks the task and raises H3.

---

## 9. Spike results that apply

| Finding | Label |
| --- | --- |
| tor-js 0.4.4 runs under Bun 1.3.14 with `MemoryStorage`; ready after 51 s with no snapshot; first request 21 s, later 4–5 s | Verified, 5 Nov |
| tor-js in Node: ready in 14.7 s cold and 2.1 s warm; a failed circuit recovers under a new isolation key | Verified, 2 Nov |
| Different isolation keys never share a circuit | Verified |
| The sidecar's RPC goes through one viem `http()` transport; payments go through `wrapFetchWithPaymentFromConfig(fetch)` | Read from source |
| Whether `@x402/hedera` makes network calls outside fetch | Open |
| Revocation in 3.7 s with a 5 s poll, without Tor | From the README |
| Revocation time with the poll over Tor | Open |
| STUN discovery and hole punching between two real NATs | Open |

---

## 10. Optional, only after every target is done

zkAPI (EF and the Open Anonymity Project) could let Bramble pay relays from a balance, so a relay learns that someone paid, not who. It adds a second payment rail, so it is a stretch at most, and only with a human's yes. Repo: https://github.com/ethereum/zkapi.

---

## 11. Risks and fallbacks

| Risk | Fallback |
| --- | --- |
| Unclear new work or thin history | "Pre-existing vs new" README from the first hour; small commits; the `pre-mumbai` tag |
| Judges see the work as polish | Lead every pitch and the README with W8, W9 and W10 |
| The AI rule questions the team's contribution | Humans approve every ADR; prompts and specs are committed; `docs/AI_USAGE.md` lists agent-written paths |
| Tor's cold start stalls the demo | Start sidecars early; the preflight checks readiness; use the snapshot bootstrap; retry under a new key |
| Hole punching fails on a specific NAT type | Automatic relay fallback; demo on cloud NATs we have tested |
| Revocation over Tor is too slow for beat 5 | Show the measured number; keep the circuit warm; tune the poll interval only within honest limits |
| Venue network blocks UDP | All nodes run in the cloud |
| `@x402/hedera` cannot go over Tor | State it on the CROPS "P" line; keep the ENS reads private |
| Live demo breaks during judging | Recorded fallback; reset script; a second rehearsed run |

---

## 12. References

- Original brief: `docs/14_MUMBAI_BUILD_BRIEF.md`; judging objections `docs/09_JUDGING.md`; track fit `docs/02_TRACK_FIT.md`
- ETHGlobal Mumbai prizes: https://ethglobal.com/events/mumbai/prizes
- ETHGlobal Mumbai rules and judging: https://ethglobal.com/events/mumbai/info/details
- tor-js: https://github.com/ethereum/tor-js
- ENS deployments: https://docs.ens.domains/learn/deployments
