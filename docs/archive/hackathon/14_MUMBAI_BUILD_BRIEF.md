# Mumbai build brief

Hand-off document for continuing Bramble at ETHGlobal Mumbai (Nov 5–7, 2026) as a continuity project. It is self-contained: everything needed to pick the project up from a fresh `git clone` is here or linked from here. Read it fully before writing code.

## 1. What this event's build is

Bramble already has a working core: ENSv2-authorized WireGuard admission, scoped service grants, live revocation, and x402 relay payments on Hedera (see `README.md` and `docs/03_ARCHITECTURE.md`). This build adds the application layer that turns those mechanisms into one flow a person can operate and a viewer can understand in seconds.

**The one-sentence pitch:** an AI agent asks for access to a private service, a human approves it in a web UI, the agent works and pays its own way, and one click revokes it. The agent never holds the backend credential.

**The problem it answers:** AI coding agents need to reach private services (staging databases, internal APIs), but handing them a credential is how credentials leak. Bramble gives an agent scoped, revocable, payable access, enforced by every peer from ENSv2 state, with no coordination server to trust.

Everything built from this brief is evaluated as new work against the baseline below. Keep that separation clean.

## 2. Ground rules

**Work window and baseline**
- Only work completed during the hackathon counts as new. The pre-hackathon baseline is commit `28e2a8f` (`chore(landing): ignore local .vercel config`). Everything after it is new work.
- Confirm the exact hackathon start time on the event page (`https://ethglobal.com/events/mumbai/info/start`, deadline Saturday Nov 7, 2026 at 09:00 IST). Do not commit feature work before the event starts. Reading, planning, environment setup and verifying that the existing checks pass are fine.
- The human may tag the baseline (`git tag pre-mumbai 28e2a8f`). Do not move or delete that tag.

**Commit hygiene**
- Commit continuously, in small meaningful units, with conventional-commit messages (`feat(approval): ...`). No single giant commit, and no batching a day's work at the end. Check `git log` shows steady history.
- Never commit or push without the human's explicit say-so for that session. Never force-push.
- Never commit secrets. `.env` is gitignored. Do not print key values, private keys or RPC URLs with credentials in logs, docs or commit messages.

**Documentation**
- The README must clearly separate what existed before the hackathon from what is new (a "Pre-existing vs new" section with the baseline commit). Add this early, then keep it current.
- Keep `docs/AI_USAGE.md` current: add the new directories and describe AI assistance honestly, as it already does. Add an ADR under `docs/adr/` for each non-trivial design decision (next number is `0011`).
- Write docs the way the existing ones are written: precise, no overclaiming. Do not reference files outside the repository.
- Describe limitations factually where they bear on the feature (for example "node keys are software keys"). Do not editorialize about the project's past state.

**Honesty rule (carried over from the existing docs)**
The claim is "no *trusted* coordinator", not "no infrastructure". Relays exist and are commodity services. Do not claim hardware-confirmed grants unless that stretch item ships and is verified on a real device. The ACL write is software-signed today; say so wherever it is described.

## 3. Demo-first method

Think problem-first, build demo-first. The final demo is the product spec; every task must trace to a beat in it. If a task does not change what appears on screen during the demo (or make a displayed claim true), it is cut or deferred. Weight effort by how much a task contributes to the demo.

### 3.1 The demo (about 90 seconds, one browser window plus one terminal)

| Beat | What the viewer sees | What must be true |
|---|---|---|
| 0. Framing (≈8s) | One line on screen: "AI agents need your private services, not your credentials." | Static text only. |
| 1. Denied (≈12s) | A real coding agent (a Claude Code session) tries to fetch a task from a private staging service. The call visibly fails: "not granted". | The agent has no backend credential. The denial comes from the gateway's live ACL check (`brambled/gateway`). |
| 2. Request and approve (≈20s) | A request card appears in the web UI: the agent's ENS name, the service it wants, the duration, a price estimate. A human clicks Approve. A transaction link appears. | The request really reaches the UI. Approve writes the scoped ACL to ENSv2 Sepolia. The UI shows the pending then confirmed state and links the transaction. |
| 3. It works and it pays (≈20s) | The identical agent call now succeeds with no restart. The UI shows the agent's spend: a Hedera USDC payment with a HashScan link. | The ACL change is picked up live. A real x402 payment on Hedera testnet settles and appears in the UI, attributed to the agent, with its cap shown. |
| 4. Revoke (≈15s) | The human clicks Revoke. The agent's next call fails again. The activity feed shows the cut-off. | Revoke writes to chain; the gateway drops the agent on its next poll (measured 3.7s with a 5s poll). |
| 5. Close (≈10s) | One line: "Scoped. Revocable. Paid. Enforced by every peer from ENS." Repo and live-URL on screen. | Static text only. |

### 3.2 Demo requirements

- The app is reachable at a real URL, not only `localhost`. A tunnel to the admin machine is acceptable if the page URL is stable. The live demo runs against real Sepolia and Hedera testnet state, with nothing hard-coded (the repo already enforces parts of this with `pnpm check:no-endpoints` and `pnpm check:no-local-state`).
- The whole flow has a single start command (see W5) and a written reset procedure so it can be rerun back to back.
- A recorded fallback of the full flow must exist before the deadline.
- Every visible element in the UI must be driven by real state. No mock data in the demo path.

## 4. Workstreams, weighted by demo value

Value is how much the item contributes to the demo beats above (3 = the demo does not exist without it, 0 = invisible to the demo). Build in descending value; cut from the bottom.

| ID | Workstream | Beats | Value | Effort | Depends on |
|---|---|---|---|---|---|
| W1 | Approval service and web UI (request queue, approve/deny, revoke) | 2, 4 | 3 | L | W2 request contract |
| W2 | Agent protagonist (a real coding agent calling the private service, and requesting access) | 1, 2, 3 | 3 | M | none |
| W3 | Agent payments surfaced in the flow (spend, cap, HashScan link per agent) | 3 | 3 | M | W1 |
| W4 | Live activity feed in the approval UI (denied, granted, revoked, paid) | 1, 3, 4 | 2 | M | W1 |
| W5 | Single-command launcher, reset script and public deployment | all | 3 | M | W1, W2 |
| W6 | README, docs, "Pre-existing vs new" section, ADRs, AI_USAGE update | none (required deliverable) | 2 | S | continuous |
| W7 | Hardware-confirmed approval signature (Ledger) | 2 | 1 | L | W1, hardware available |
| W8 | STUN endpoint discovery and automatic relay fallback | none | 0 | XL | none |

**Cut order:** W8 first, then W7, then the polish parts of W4. W1, W2, W3 and W5 define the minimum viable demo; do not start W7 or W8 until the minimum viable demo has been recorded end to end.

## 5. Workstream specifications

### W1. Approval service and web UI

**Placement.** Create a new workspace package (suggested: `approval/`) and register it in `pnpm-workspace.yaml`. Do not extend `dashboard/`. The dashboard is deliberately read-only and never signs (see `dashboard/README.md`); that property is a documented guarantee and must stay true.

**Stack.** Match `dashboard/`: Next.js 16, React 19, Tailwind 4, shadcn/radix. `dashboard/AGENTS.md` warns that this Next.js version differs from older releases; read `node_modules/next/dist/docs/` before writing Next code. Reuse the dashboard's visual language so the two surfaces look like one product (`dashboard/src/components`, `globals.css`).

**Responsibilities**
1. Accept access requests from an agent: `POST /api/requests` with `{ agentLabel, gatewayLabel, service, note?, requestedDuration? }`. Store pending requests in memory (the approval service is an operator tool, not part of the node runtime path covered by `check-no-local-state`; keep it that way, and do not add persistent local state to `brambled/` or `sidecar/src/`).
2. Show each request with identity details resolved from ENS (the agent's label, whether its pubkey is published, whether it is revoked) so a human approves a verified identity, not just a string.
3. On Approve, perform the same on-chain grant `admincli/src/set-acl.ts` performs (ECDH digest via `admincli/src/acl-grant.ts` and `admincli/src/roles.ts`, written with `setText` to the agent's resolver). On Deny, record the decision and notify the agent.
4. On Revoke, perform what `admincli/src/revoke-acl.ts` (ACL only) or `admincli/src/revoke.ts` (device) performs.
5. Expose request status to the agent: `GET /api/requests/:id`.

**Refactor needed.** The `admincli` scripts are CLIs. Extract the grant and revoke logic into importable functions inside `admincli` (keep the CLI entry points working, and keep `admincli/test/*` passing), and call those functions from the approval service. Do not copy-paste the cryptography.

**Signing and secrets.** The granter's ACL key is decrypted per grant from a Ledger Key Ring file on the provisioned admin machine (`docs/adr/0001-ledger-ring-vs-send-split.md`, `admincli/src/ledger-ring.ts`); the on-chain write is signed by the operator key (`SEPOLIA_PRIVATE_KEY`). The approval service must therefore run on that admin machine, and the web UI can be served through a tunnel. Isolate signing behind a small interface so tests can substitute an in-memory signer and resolver (the integration test `brambled/gateway/server_test.go` already does the equivalent for the Go side).

**Duration.** ACL grants in ADR 0005 carry no expiry. If the UI offers a duration, implement it as a server-side timer that performs the revoke when it elapses (and show the countdown); otherwise omit duration from the UI. Do not imply on-chain expiry that does not exist.

**Security notes to keep honest in docs.** A request is an unauthenticated suggestion; the approver's decision is the authority. The UI must display ENS-resolved identity facts and must not auto-approve. Bind the approval service to loopback or a tunnel with a shared token; it is a write surface.

**Acceptance**
- With a fresh agent identity and no grant, a request appears within 2 seconds of the agent submitting it.
- Approve produces a confirmed Sepolia transaction, linked in the UI, after which the agent's call succeeds on the gateway's next resolve with no restart.
- Revoke produces a confirmed transaction after which the agent's call fails again.
- Approving twice, or revoking an ungranted agent, fails with a clear error and no chain write.

### W2. Agent protagonist

**Goal.** The demo's agent is a real coding agent (a Claude Code session), not a scripted curl. It should be obvious to a viewer that an AI agent is acting.

**Suggested approach.** Provide a small MCP server (new package, for example `agent-mcp/`) exposing two tools the agent can call: `request_private_access(service, reason)` which submits to the W1 API and polls for the decision, and `call_private_service(path)` which hits the Bramble forward on `127.0.0.1:<port>` (the same path `brambled/cmd/demo-agent` uses). The tools return plain, readable outcomes ("denied: no grant", "granted", "task result: ...") so the agent's narration is understandable on camera. Keep `brambled/cmd/demo-agent` and `cmd/demo-service` working; they are the existing, tested baseline path.

**Invariants.** The agent has no backend credential at any point. The forward and the gateway ACL are the only route to the service. Document in the README how to attach the MCP server to Claude Code.

**Acceptance**
- A fresh Claude Code session with the MCP server attached can run the full deny → request → (human approves) → succeed → (human revokes) → denied sequence unattended apart from the human's clicks.
- The agent's identity in the UI matches the ENS label the node is enrolled under.

### W3. Agent payments in the flow

**What exists.** `sidecar/src/payments/` pays rendezvous tokens and data-relay sessions over x402 through Blocky402 on Hedera testnet; per-payment and per-session caps are enforced (`HEDERA_MAX_PAYMENT_ATOMIC`, `-max-relay-price-per-byte`, `-max-relay-session-cost`). The dashboard already has a `/payments` page backed by `dashboard/src/app/api/hedera/payments/route.ts` (mirror-node feed).

**What to add.** Surface payments in the approval UI attributed to the agent: payer account, cap, total spent, remaining, and a HashScan link per settlement. Make sure a real paid request happens inside the demo window (for example, the agent node's connection attempt triggering a rendezvous payment, or an explicit relay session) and is visible within a few seconds. Verify against the real sidecar behavior before designing the UI; do not assume when payments fire.

**Acceptance.** During the demo flow, at least one real settlement appears in the UI with a working HashScan link, a visible cap, and no human involvement in the payment step.

### W4. Activity feed

The node already exposes a loopback status API (`brambled/statusapi`, proxied for the dashboard at `/api/brambled/activity`). Show a unified feed in the approval UI: request received, denied by gateway, granted, payment settled, revoked, peer dropped. Reuse the existing activity events; add events only where a beat would otherwise be silent. (Note: `dashboard/README.md` says brambled has no HTTP surface; that predates `statusapi` and may be corrected as part of W6.)

### W5. Launcher, reset and deployment

- One command (for example `pnpm demo`) that starts what is needed on the machine it runs on and prints what to start on the others, plus a documented topology: admin machine (approval service, signing), gateway machine (`brambled serve` with `-service`, private backend `demo-service`), agent machine (`brambled serve` with `-forward`, the agent). See the runbook in `brambled/README.md` ("Client forwarding runbook") and the demo path in the root `README.md`.
- A reset script that returns the demo to the initial state (agent enrolled, no ACL) using real chain operations, so the demo can be re-run repeatedly.
- A preflight script that checks: required env vars present (names only, never values), `SEPOLIA_RPC_URL` is a dedicated endpoint, Hedera payer balance and USDC association, relay reachable, sidecar healthy, device registry resolves.
- Public URL for the approval UI (tunnel or hosted front-end). Document it in the README.

### W6. Documentation

- README: add "Pre-existing vs new", the demo flow, how to run the new pieces, and update the architecture section for the new package(s).
- ADRs for: placement of the approval service (separate from the read-only dashboard), the request contract, grant duration handling, and the MCP agent interface.
- `docs/AI_USAGE.md`: add new paths and describe AI assistance honestly.
- Keep claims precise and consistent with section 2's honesty rule.

### W7 (stretch). Hardware-confirmed approval

Goal: the Approve action requires a physical confirmation on a Ledger device. `admincli/src/ledger-eth-sign.ts` already signs enrollment transactions through `@ledgerhq/hw-app-eth` with on-device confirmation, so the technique exists. Do not start until the minimum viable demo is recorded. It requires physical hardware; a cloud environment cannot verify it, so stop at "implemented behind a flag, untested on device" and say so rather than claim it. Record the real verification only when the human has run it on a device.

### W8 (stretch, likely skip). STUN and automatic relay fallback

Documented as unimplemented in the README. Not on the demo path. Do not start.

## 6. Existing system map

| Path | What it is |
|---|---|
| `brambled/` | Go node daemon: WireGuard (`wgnode`), ENS-driven admission (`admission`), CONNECT gateway and ACL enforcement (`gateway`), localhost forwards, rendezvous client (`rendezvous`), data relay (`datarelay`), loopback status API (`statusapi`), helper commands `cmd/demo-service`, `cmd/demo-agent` |
| `sidecar/` | Bun/TypeScript loopback ENS and payment client for one node (`src/ens`, `src/payments`, `src/routes`) |
| `relay/`, `relay-sidecar/` | Go rendezvous/data relay and its x402 resource server |
| `admincli/` | ENSv2 writes: `enroll`, `rotate`, `revoke`, `set-acl`, `revoke-acl`, `set-acl-granters`, `grant-role`, relay registry setup, Ledger signer and Key Ring |
| `dashboard/` | Read-only Next.js view (devices, activity, payments, registry, relays). Must stay read-only |
| `landing/` | Static landing page |
| `scripts/` | `check-no-endpoints.mjs`, `check-no-local-state.mjs`, dev-tailnet provisioning, gate checks |
| `docs/` | Spec set `00`–`13`, ADRs `0001`–`0010`, sources, AI usage |

Read first: `README.md`, `docs/03_ARCHITECTURE.md`, `docs/adr/0005-acl-record-schema.md` (grant mechanics), `docs/adr/0006-gateway-connect-protocol.md`, `docs/adr/0001-ledger-ring-vs-send-split.md`, `brambled/README.md`, `dashboard/README.md`.

## 7. Invariants (do not break)

1. **Peers check admission and ACL, never a central service.** Nothing in the approval service may become an enforcement point. It only writes chain state that peers independently read.
2. **No topology on-chain for mesh members.** Device and agent records never carry endpoints, IPs or ports (`pnpm check:no-endpoints`).
3. **No local-state writes in the node runtime path** (`brambled/**/*.go` non-test, `sidecar/src/**`) (`pnpm check:no-local-state`).
4. **`dashboard/` stays read-only** and never signs or forwards payment calls.
5. **ACL records are digests of symbolic service names**, never plaintext names, hosts or ports (ADR 0005).
6. **The agent holds no backend credential.**
7. **`pnpm verify` passes** (format, endpoint and local-state checks, typecheck, lint, build, test) before every push. CI runs it on every push (`.github/workflows/ci.yml`).
8. **Honest claims**: see section 2.

## 8. Environment

**Tooling:** Node.js 22, pnpm 10 (`packageManager` pins it), Bun, Go 1.27. Install with `pnpm install --frozen-lockfile`. Verify with `pnpm verify` and `pnpm test:access-flow` (rootless; uses userspace WireGuard and an in-memory resolver).

**Configuration** (see the README's table; values live in an ignored `.env` supplied by the human, never committed): `BRAMBLE_TAILNET_NAME`, `BRAMBLE_TAILNET_REGISTRY`, `BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK`, `BRAMBLE_RELAY_REGISTRY`, `BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK`, `BRAMBLE_PRIVATE_KEY`, `SEPOLIA_RPC_URL`, `SEPOLIA_PRIVATE_KEY`, `HEDERA_CLIENT_ACCOUNT_ID`, `HEDERA_CLIENT_PRIVATE_KEY`, `HEDERA_MAX_PAYMENT_ATOMIC`. Use a dedicated Sepolia RPC endpoint; the default public pool has shown multi-thousand-block lag between backends.

**What an agent running in a cloud sandbox can and cannot verify**
- Can: build, typecheck, lint, unit and integration tests, `pnpm test:access-flow`, the approval service and UI against an in-memory signer/resolver, UI screenshots.
- Cannot: real OS TUN WireGuard (needs root on real machines), the physical Ledger, funded Sepolia/Hedera transactions without the human supplying secrets, multi-machine runs. For these, write the code and the runbook step, mark the item "needs human verification" in the PR/commit notes, and never claim it passed.

## 9. Build order

1. Baseline check: `pnpm install --frozen-lockfile`, `pnpm verify`, `pnpm test:access-flow`. Record results.
2. Add the README "Pre-existing vs new" section and the ADR for approval-service placement.
3. Refactor `admincli` grant and revoke into importable functions; keep CLIs and tests green.
4. W1 request API and in-memory flow with a test signer; then the UI shell.
5. W2 MCP server and the end-to-end deny → request → approve → succeed → revoke loop in the rootless integration test (extend `test:access-flow` or add a sibling test).
6. W3 payments surfaced; W4 feed.
7. W5 launcher, reset, preflight, public URL.
8. Human runs the full flow live on real machines; record the fallback video the same day.
9. W6 docs finalized. Then, only if time remains, W7.

## 10. Definition of done

- [ ] Full 90-second flow runs from one command against live Sepolia and Hedera testnet, twice in a row after a reset.
- [ ] Every beat in section 3.1 is true, with nothing hard-coded or mocked in the demo path.
- [ ] A real settlement with a working HashScan link appears in the UI attributed to the agent.
- [ ] The approval UI is reachable at a public URL.
- [ ] Fallback recording exists.
- [ ] `pnpm verify` and CI are green; the README separates pre-existing from new; ADRs and `docs/AI_USAGE.md` are current.
- [ ] Commit history shows steady incremental work across the hackathon window.
- [ ] No secrets, private IPs or credentials in the repository.

## 11. Open questions for the human

1. Exact hackathon start time, and whether the repo should be tagged `pre-mumbai` at `28e2a8f`.
2. Which machines play admin, gateway and agent during the live demo, and where the public URL terminates.
3. Whether grant duration is wanted in the UI (server-side timer) or omitted.
4. Whether the Nano S Plus will be available in time for W7.
5. Whether to run the approval UI behind a hosted front-end or a tunnel.
