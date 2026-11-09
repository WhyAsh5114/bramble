# Onboarding and cloud access acceptance, 6 November 2026

Reference for ETH-184 / ETH-177, demo beats 1–5. Evidence cutoff: 16:30 UTC on 6 November 2026.

## Verdict and provenance

**Read from recorded evidence:** real public installation and enrollment pass on non-cloud Linux amd64. The existing cloud worker passes deny, approve, HTTP 200 and revoke twice. The fresh device fails connectivity and service access; the full outside-cloud acceptance sequence does **not** pass. Browser observations, relay-only access, hole punching and the live rehearsal harness remain **Open**.

This is a docs-only reconciliation requested by the operator at 16:26:57 UTC. Hedera-Sol read the saved documents and comments below; no installation, browser session, cloud operation, grant, payment or acceptance test ran for this reconciliation. “Read from recorded evidence” attributes the original operators' observations; “Read from source” describes a procedure; “Verified” is reserved for checks actually run by this author and recorded in the PR.

| Record                                                                                                                              | Scope                                                                                                                   |
| ----------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| [ETH-179 installer acceptance](/ETH/issues/ETH-179#document-acceptance), revision `5716c8b3-7838-41f3-aee2-95380f686fc2`            | Installer, HTTPS hosting and platform limits                                                                            |
| [ETH-250 public-device acceptance](/ETH/issues/ETH-250#document-public-acceptance), revision `eea219ac-5216-4c5a-aee2-c2f250486b25` | Exact headless command, approved enrollment, failed service test and browser limits                                     |
| [ETH-244 post-fix validation](/ETH/issues/ETH-244#document-validation-postfix), revision `435e5338-9634-4d4b-ab03-7a104745e54e`     | Two cloud passes, fresh-device failure, release hashes and cleanup                                                      |
| [ETH-244 installation how-to](/ETH/issues/ETH-244#document-install-howto)                                                           | Human command and expected UI flow                                                                                      |
| [ETH-287 completion, 15:54 UTC](/ETH/issues/ETH-287#comment-76b5b77e-5db0-43a2-9d9f-350b53acd77e)                                   | Rehearsal harness merged in [PR #55](https://github.com/WhyAsh5114/bramble/pull/55); offline validation, no live result |
| [ETH-244 shutdown, 16:11 UTC](/ETH/issues/ETH-244#comment-7bd09a00-c17c-4423-8988-a51612d01c00)                                     | Final restored revision and stopped gateway/worker                                                                      |

The original [29-file sanitized archive](/api/attachments/52c4d76f-4ad8-4806-a14a-eb392ad3182a/content?download=1) is the raw-evidence attachment to ETH-244. This reconciliation uses the saved report; it does not claim a new inspection or execution of every archived file.

## Non-cloud acceptance log

**Read from recorded evidence:** the release is `970461c4956e16808ef9d40495f4b3b6dac4872d`. All four published archives have SHA-256 hashes matching the build. The runtime test uses zephyrus, outside the cloud machines, with a clean HOME and Linux amd64. Linux arm64 and both macOS targets are build-only. The real release contains three executables: `bramble`, `brambled` and `bramble-sidecar`.

The executed command is:

```sh
curl -fsSL https://console.13-233-181-104.sslip.io/install.sh | sh -s -- --no-open
```

| Acceptance step                    | Recorded observation                                                                                                  | Result                                                                     |
| ---------------------------------- | --------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| Download and install               | HTTPS download, checksum verification and installation succeed without Go, Bun, Node.js, pnpm or sudo                 | Pass, reported                                                             |
| Local UI                           | Embedded UI starts at `http://127.0.0.1:7777`                                                                         | Startup passes; automatic browser opening and completed rendered flow Open |
| Request membership                 | Label `eth244-postfix-device`, tailnet `bramble-dev-c91e55e9.eth`, join `a44724b1-6253-4b98-8b6e-cf7bac8dd99d`        | Pass, reported                                                             |
| Operator approves enrollment       | Approved at 15:34:54.072 UTC; mesh address `10.77.0.7/24`                                                             | Pass, reported                                                             |
| Operator grants `task` access      | Request `8cbf1eb7-6eb3-45cb-97d3-ddf2d1d46860`, approval confirmed at 15:44:40.938 UTC                                | Pass, reported; separate from enrollment                                   |
| Connected peer                     | After more than eight minutes, phase remains `approved`; every peer path is `none`, no handshake, zero received bytes | Fail                                                                       |
| Private-service test               | `POST /api/test` returns `{"ok":false,"status":0,"ms":95000}`                                                         | Fail; no target HTTP 200                                                   |
| Relay-only still works             | No working fresh-device path is established                                                                           | Open / not passed                                                          |
| Revoke stops working device access | Service and membership revocations are confirmed, but the device never established working access                     | Cleanup passes; enforcement transition Open                                |

Recorded receipts:

| Operation                           | Sepolia transaction                                                  |
| ----------------------------------- | -------------------------------------------------------------------- |
| Enrollment deploy                   | `0x8556a188ba43e9aea7700b588781045dfc239c150436089afc5e630cad67cca1` |
| Enrollment register                 | `0xd8276c37edb95fd4bdb4cb74edc8b27372b8453649618ff0b9c97b67f56115f3` |
| Enrollment public-key write         | `0xdca0ef8e67866db6855c49d2918c2b47ac054d968bf5e16e030cc41b4fd70f12` |
| Mesh-IP write                       | `0x3826201204d770007915efc2f916f928f121f9206accf2f6b2837bf156b300b9` |
| Service grant                       | `0xa96a411230a034587916a4a3fbd51fad77fc6bb181999a6986e878d5b79c5482` |
| Service revoke, 15:46:51.780 UTC    | `0x4a1e944368ab0971cebd5678b4781d759f5d03e2ca20d4dc48731c65776ab9aa` |
| Enrollment revoke, 15:47:16.444 UTC | `0x027550f22cdafb70f4844363959bf51b5e1d3e0571e8afdb3743b65500eeb261` |

The owned local daemon and helper stop, their PIDs disappear, and port 7777 refuses connections. Do not reuse the revoked label. This is local-process cleanup evidence, not a fresh full uninstall of the real bundle. Historical public-host install/uninstall used a stub bundle; it does not extend real-binary runtime coverage. The saved records do not provide a complete install-to-join duration or a successful browser walkthrough.

## Existing cloud worker: two access-lifecycle passes

**Read from recorded evidence:** gateway, worker and relays run `970461c`. Services start at 15:32:36–15:32:38 UTC; no node restarts occur between passes. The worker runs curl as Unix user `agent` through `http://127.0.0.1:8080/task`. This is not an autonomous AI agent session. The gateway confirms the worker's ACL empty before pass A. Denial is curl exit 56 / reset correlated with gateway enforcement; allowed calls return HTTP 200 and `report-private-service-health`.

| Observation                | POSTFIX-A                              | POSTFIX-B                              |
| -------------------------- | -------------------------------------- | -------------------------------------- |
| Start UTC                  | 15:38:09.599                           | 15:41:57.276                           |
| Initial denial             | 121.0 s                                | 26.4 s                                 |
| Approval confirmed         | 19.5 s                                 | 17.0 s                                 |
| First allowed call         | 40.9 s                                 | 39.1 s                                 |
| Revoke confirmed           | 16.9 s                                 | 20.2 s                                 |
| Revoke-confirmed to denial | 29.2 s                                 | 30.7 s                                 |
| Revoke-started to denial   | 46.1 s                                 | 50.8 s                                 |
| End UTC                    | 15:41:57.276                           | 15:44:10.963                           |
| Request ID                 | `e4db565b-aff1-4796-8cbe-a4ce56150b6f` | `9ddb039e-913f-47c6-888c-4619f276b203` |

These are call-completion bounds including ENS reads over Tor, not isolated Tor overhead or warm decision latency. The first denial includes startup delay; rendezvous timeouts appear in gateway logs without an isolated cause. The cloud peers establish a direct endpoint. That does not verify hole punching across separate NATs or relay-only public-device service access. Both cloud test grants are revoked with confirmed receipts in the source beat log.

The historical granter failure on [ETH-205](/ETH/issues/ETH-205) is not asserted to remain a blocker: the later passes record confirmed grants and working cloud calls. They do not establish new-device acceptance. Hole punching remains a separate evidence requirement on [ETH-39](/ETH/issues/ETH-39).

## Payment, feed, harness and shutdown limits

**Read from recorded evidence:** one bounded Tor payment diagnostic receives HTTP 402, then a signed retry succeeds with HTTP 200 and PAYMENT-RESPONSE. It does not reproduce or explain the earlier paid-token rejection on [ETH-276](/ETH/issues/ETH-276). It is separate from a new task-call payment or HCS display acceptance.

At 15:42:43 UTC, public `/api/activity` reports approval `ok`, gateway `not_configured` and Hedera `not_configured`. Approval transactions appear; full gateway/payment/HCS feed acceptance remains Open on [ETH-294](/ETH/issues/ETH-294).

**Read from source and ETH-287:** `deploy/scripts/rehearse.py` drives beats 1, 2, 3 and 5 and reports timings and exit status. Its owner records offline simulation and 11 local happy/fault scenarios. No live harness run is recorded. Unlike the narrower cloud access passes, harness beat 3 also requires a fresh payment link and a posted HCS grant receipt; HTTP 200 alone cannot pass it. See the [rehearsal how-to](../../deploy/REHEARSAL.md).

**Read from the final shutdown comment:** the gateway stops at 16:10:07 UTC and the worker at 16:10:11 UTC. Both report `ActiveState=inactive`, `SubState=dead`, `MainPID=0`, with clean checkouts and rebuilt executables restored to `970461c`. Temporary flag drop-ins are removed. The later OFF/ON timing runner stops before creating a request because the existing approval-token environment is absent; those four timing passes remain Open on [ETH-271](/ETH/issues/ETH-271). No running pair or new slot is implied by these docs.

## Existing follow-ups

| Missing acceptance                                                                                         | Existing ticket                                                   |
| ---------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| Fresh-device handshake, connected path, service HTTP 200, relay-only access and observed revoke transition | [ETH-293](/ETH/issues/ETH-293), the continuation named in ETH-250 |
| Public gateway/payment/Hedera activity sources and display evidence                                        | [ETH-294](/ETH/issues/ETH-294)                                    |
| Separate-NAT direct path, hole punching and fallback                                                       | [ETH-39](/ETH/issues/ETH-39)                                      |
| Controlled OFF/ON latency comparison                                                                       | [ETH-271](/ETH/issues/ETH-271)                                    |

These existing tickets track the recorded failures; this docs-only closeout creates no duplicate repair work. The [demo script](../DEMO_SCRIPT.md) uses the installer for enrollment where it fits, retains the accepted cloud worker for service access, and keeps the unaccepted outcomes explicit.
