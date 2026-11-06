# Public console QA evidence, 6 November 2026

This reference records the ETH-249 read-only browser pass for demo beats 1, 2,
3 and 5. The console responds, but the pass does not establish full demo
readiness. Two P1 deployment findings and one P2 theme finding remain.
The latest fresh-browser check at 13:35 UTC cannot load the console assets.

## Conditions

- Operator: QA-Sol.
- Observation window: 2026-11-06, approximately 12:33–12:38 UTC.
- Public URL: `https://console.13-233-181-104.sslip.io/`.
- Browser: `agent-browser`, launched with `AGENT_BROWSER_ARGS="--no-sandbox"`.
- Viewports: 1280 × 900 and 390 × 844 CSS pixels, device scale 1.
- Themes: emulated `prefers-color-scheme: light` and `dark`.
- The public console stayed open for more than 60 seconds before the final
  history and source-health observations. No token was entered. No request,
  approval, denial, grant, revoke, join or setup action was submitted.
- No cloud-node shell, service, configuration or deployment was accessed.
- Local reference checkout: `5724e2f` from `origin/hackathon`. **Open:** the
  deployed commit SHA is unavailable from the UI; this checkout SHA does not
  identify the deployed build.

## Coverage

| Segment | Evidence and verdict |
| --- | --- |
| Queue, beat 2 | **Verified:** live refresh label, 0 pending requests, explicit empty state. A populated pending queue remains **Open**. |
| Request cards, beat 2 | **Verified:** eight closed requests render identity labels, public-key/revocation/expiry summaries, service, duration, missing-quote copy and history status. |
| Approve and grant, beats 2–3 | **Verified:** existing history displays confirmed grant transaction links. **Open:** approve controls, confirmation transitions and populated active-grant list; no eligible current requests or active grants exist. No action was submitted. |
| Revoke, beat 5 | **Verified:** history displays revoked status and separate confirmed revoke transaction links. **Open:** active revoke controls and actual enforcement/cutoff. Receipt text is UI evidence, not independent chain or gateway verification. |
| Activity, beats 1–3 and 5 | **Verified:** existing request, deny, grant and revoke events render. Approvals reports live; gateway and payments report not configured. P1 finding below. **Open:** arrival of a new event, payment settlement and gateway cutoff. |
| HCS/HashScan, beat 3 | **Verified:** live panel explicitly requires unlocking. `/audit/fixtures` displays grant, payment and revoke records with commitment-match labels, topic, sequence, timestamp and HashScan URLs. The page explicitly labels synthetic events and URL-format examples. **Open:** authenticated retained records, live mirror matching and actual HashScan transaction existence. No synthetic link is counted as a live receipt. |
| Layout | **Verified:** captured public pages and local mock setup remain within the viewport at both widths. Dark preference retains the light palette. P2 finding below. |
| Local join/status/peers, beats 2 and 4 | **Verified:** loopback ports 7777 and 7778 refuse connections. Existing mock UI at `http://127.0.0.1:7788/` displays “Name this device” at both widths/themes. **Read from source:** `local-ui/README.md` identifies 7788 as the mock and 7777 as the embedded daemon UI. **Open:** join, connected status, peers and admin views. The existing mock was not advanced or reset. |

## Ranked defects

No P0 was observed within the inspected read-only states. Uninspected actions
and live integrations are not covered by that statement.

### P1: gateway and payment evidence is absent from the public feed

**Verified.** Filed as [ETH-254](/ETH/issues/ETH-254). This prevents the feed
from presenting gateway allow/deny, settlement and cutoff evidence for demo
beats 1, 3 and 5. The infrastructure cause remains **Open**.

Exact reproduction:

1. Open the public URL at 1280 × 900 or 390 × 844.
2. Wait at least 60 seconds and read `Live activity` → feed sources.
3. Observe `Approvals: live`, `Gateway: not configured` and
   `Payments: not configured`.
4. Inspect the existing events. Request/deny/grant/revoke events are present;
   gateway/payment/cutoff events are absent.

Expected: the intended public-demo sources supply existing gateway and payment
evidence, with source-health labels preserved. Reproduces with either theme
preference. Evidence: `1280-light-top.png`, `390-light-top.png` and
`live-snapshot.txt` in the evidence directory.

### P2: dark preference retains the light palette

**Verified.** Both public pages and the local mock setup render a white
background with dark text when the browser requests dark colors. Readability
remains intact; this is a theme-coverage gap.

Exact reproduction:

1. Open the public root or `/audit/fixtures`.
2. Set viewport to 1280 × 900 or 390 × 844.
3. Run `agent-browser --session <session> set media dark`.
4. Evaluate `matchMedia('(prefers-color-scheme: dark)').matches` and
   `getComputedStyle(document.body).backgroundColor`.
5. Observe `true` and `oklch(1 0 0)` respectively. The screenshots retain the
   light palette. Switching to `set media light` retains the same background.

Expected for a dark-theme pass: a dark palette follows the requested color
scheme. Evidence: each `*-dark-*.png` and its corresponding light capture.
Dark theme support as a product requirement remains **Open**; this report
records the requested test condition without choosing a new visual design.

## Screenshot paths

The evidence directory is `artifacts/eth-249/` under
`/home/anonjr/bramble-work/wt/qa-sol/eth-249-public-qa/`. Images are delivered
through the Paperclip evidence bundle; they are not committed as source.

| Page/state | 1280 light | 1280 dark | 390 light | 390 dark |
| --- | --- | --- | --- | --- |
| Public live console, full page | `1280-light-live.png` | `1280-dark-live.png` | `390-light-live.png` | `390-dark-live.png` |
| Public HCS offline fixtures, full page | `1280-light-fixtures.png` | `1280-dark-fixtures.png` | `390-light-fixtures.png` | `390-dark-fixtures.png` |
| Local mock setup, full page | `1280-light-local-mock.png` | `1280-dark-local-mock.png` | `390-light-local-mock.png` | `390-dark-local-mock.png` |

Focused captures: `1280-light-top.png`, `390-light-top.png` and
`1280-light-hcs-locked.png`. Text evidence: `live-snapshot.txt`,
`live-text.json`, `local-mock-observations.json` and `matrix-observations.json`.

## Approval boundaries

**Open:** ETH-51 H4 human approval, deployed commit identity, authenticated live
HCS evidence and the uninspected action states listed above. ETH-249 provides
the requested read-only evidence; it does not replace those checks or authorize
new live actions.


## Continuation, 13:27–13:29 UTC

**Verified, 6 November 2026:** QA-Sol continues the same read-only pass after
the operator moves the task to Codex. The original observations and 15-image
bundle above remain dated evidence. The continuation uses a fresh unauthenticated
`agent-browser` session with `AGENT_BROWSER_ARGS="--no-sandbox"`.

- At 13:28:08 UTC, the public summary shows 0 pending requests, 1 active grant
  and 11 closed requests. The populated grant card renders `mumbai-worker`,
  `mumbai-gateway`, identity summaries, `Grant confirmed` and a Sepolia
  Etherscan link. **Open:** authenticated revoke controls; the locked page has
  only the disabled `Unlock` button and no grant-card action button.
- By 13:28:39 UTC, passive polling shows 0 active grants and 12 closed requests.
  The feed adds `REVOKED`, `Revoke transaction confirmed` and a receipt link.
  This verifies an arriving approval-source event and the grant-list transition
  during observation. It does not independently verify chain state or gateway
  enforcement. QA-Sol submits no action.
- After 60 seconds, `Approvals: live`, `Gateway: not configured` and
  `Payments: not configured` still reproduce the P1 in
  [ETH-254](/ETH/issues/ETH-254).
- A fresh 1280 × 900 / 390 × 844, light/dark matrix has no horizontal overflow.
  Both dark captures retain `oklch(1 0 0)` on the body. The P2 remains.
- The live HCS panel still requires unlocking. Authenticated audit records,
  real approve/revoke actions and enforcement remain **Open**. Local UI
  availability is not retested in this continuation.

Continuation evidence is under `artifacts/eth-249/continuation/` in the same
worktree: `observations.json` and `transient-observations.txt`. The transient
populated grant is text evidence only. The four earlier grant-list crops are
excluded from the supplementary evidence bundle because the mobile crops are
blank and do not establish a rendered grant-list state.

**Verified:** PR #45 remains open against `hackathon`, with no completed review.
The configured native reviewer, Core-Claude, is paused. Cross-family review
round 1 remains pending; this continuation does not count as a review round.
The operator's pause window is approximately 16:30 UTC. QA-Sol owns retrying
the existing review handoff after the reviewer becomes invokable. No cloud-node
access, code change, deployment, merge or real approval/revocation occurs.


## Fresh-browser regression, 13:34–13:35 UTC

**Verified, 6 November 2026:** a fresh unauthenticated browser first shows
“This page couldn’t load.” After one reload, it renders an unstyled document
that remains at `Connecting…`, `Loading access requests…` and
`Loading activity…`. Browser resource timing and direct HTTP requests both
report HTTP 500 for these assets referenced by the served HTML:

- `/_next/static/css/6fe73cb2f182d340.css`
- `/_next/static/chunks/app/page-cc16018eb0b52f23.js`

The root HTML, `/api/activity` and the other sampled script assets return
HTTP 200. `/api/payments` returns HTTP 503. HTML and API reachability do not
establish frontend readiness. The cause of the asset failures remains
**Open**; QA does not inspect or change the deployment.

### P1: the public console cannot load its stylesheet and page script

Filed as [ETH-261](/ETH/issues/ETH-261), assigned to Infra-Sol-UI, the
current cloud-validation owner. This affects demo beats 1, 2, 3 and 5.

Exact reproduction:

1. Open the public root in a fresh browser, then reload once.
2. Observe the unstyled document and loading placeholders.
3. Request each asset path above directly. Both return HTTP 500 with
   `Content-Type: text/plain`.

Expected: all assets referenced by the served HTML load successfully and the
console hydrates, polls and displays history. Earlier styled screenshots remain
dated evidence and do not establish current readiness.

The supplementary evidence bundle contains full-page screenshots at 1280 × 900
and 390 × 844, with light and dark preferences:
`1280-light-checked-full.png`, `1280-dark-checked-full.png`,
`390-light-checked-full.png` and `390-dark-checked-full.png`. Their dimensions
and visible content are checked. These capture the degraded document; they do
not demonstrate a working console or a functioning theme.
`resume-checked-observations.json` records the document text and viewport widths.
`public-asset-responses.json` records the direct HTTP observations.

No real decision, authentication, cloud-node access or deployment is performed.
Cross-family review of PR #45 remains pending while Core-Claude is paused.
