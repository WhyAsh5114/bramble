# Recover when the live cloud demo fails

Use this plan when a cloud node, the public console, Tor or Sepolia fails during
the demo. It tells you what to record beforehand, how to decide to switch, and
which offline runs stand in for each beat. It needs no cloud node to prepare
the offline parts. Handoff section 4 requires a full recording before T-6; this
page is the recipe for it.

**Evidence labels.** **Verified** means this author ran it on 6 November 2026
at base `52a35cb`. **Read from source** means taken from the repository and
never run end to end. **Open** means untested. Nothing here has run against the
cloud pair; ETH-313 now owns the exclusive live slot.

## Use the agent backup through the public console

If the new-device path misses a stage deadline, use the
[agent backup](../DEMO_SCRIPT.md#fall-back-to-the-agent-path). Worker MCP sends
approval requests to `https://get-bramble.whyash5114.com`. The operator
reported this route on 6 November 2026 at 18:44 UTC; a new Pi lifecycle through
that URL remains **Open** for this author.

1. Before the slot, follow the [Pi installation step](../../agent-mcp/PI.md#steps)
   with `BRAMBLE_APPROVAL_URL=https://get-bramble.whyash5114.com`. The
   installer saves it in Pi's MCP configuration. Rerun that step for an existing
   install that used `http://127.0.0.1:3001`.
2. Run the [worker reachability check](../DEMO_SCRIPT.md#close-with-the-agent).
   Expect HTTP 200. The agent beat needs no worker reverse tunnel. Keep the
   workstation's `bramble-console-tunnel` running: it serves the public URL.
   Private service calls still use the worker's `http://127.0.0.1:8080` forward.
3. Run `~/bin/pi-agent -p "Fetch the task from the private staging service"` as
   `agent` on the worker. The human approves and revokes through the console.
   After revoke, run a separate Pi invocation asking it to call `/task` once
   without requesting access, and check the fresh gateway denial.
4. If the public console is unavailable, switch to a labeled recording or the
   offline stand-ins below. The agent backup also depends on that URL; recreating
   the old worker tunnel is not a stage recovery step.

ETH-243's two recorded Pi lifecycles used a temporary worker tunnel. Keep that
historical routing and their measurements labeled when playing the recording.

## What you can and cannot replace offline

| Beat | Offline stand-in | What it proves | What it does not prove |
| --- | --- | --- | --- |
| 1 Denied, 5 Revoke | `pnpm test:access-flow` | Verified: a loopback WireGuard pair denies, allows, then denies after revoke. Printed revoke-to-denial: 2.5 ms and 3.7 ms in two runs. | Tor delay, Sepolia, a cloud node. Do not quote these figures as the demo's revoke time. |
| 1, 2, 3, 5 sequence | `python3 deploy/scripts/rehearse.py --offline` | Verified: the harness runs its checks in order. Output starts `SIMULATED`. | Any access, payment or chain write. Never show it as evidence. |
| 6 Provider view | The capture in [beat6-provider-view.md](beat6-provider-view.md) | Verified: the logging proxy records every ENS query with time, source address and parameters; the private payload is absent. | A real Tor exit. Both phases log `127.0.0.1`. |
| 4 Direct path | None. Use only a pre-recorded clip. | n/a | `deploy/nat-lab/README.md` needs a NAT box or Docker; its steps are Open. |
| 3 Payment, HCS record | None. Use only a pre-recorded clip. | n/a | A settlement or HCS receipt needs the live chains. |

## Decide when to switch

Use the action deadlines in [DEMO_SCRIPT.md](../DEMO_SCRIPT.md#recover-when-a-beat-stalls).
The latest ETH-244 worker observations are 121.0 / 26.4 s for denial, 40.9 / 39.1 s
for success, and 46.1 / 50.8 s from starting revoke to the first failed call.
These are individual observations, not limits or measurements of the new public
MCP route.

1. Switch at the script's action deadline or clock-window end. If the result has not appeared, say
   “I will switch to the recording” and start the matching clip.
2. Switch at once, without a retry, if the console URL fails to load, a node
   unit is inactive, or a call returns an error that is not
   `denied for service "task"`. A reset or timeout without that denial line
   proves nothing.
3. Resume live only at a beat boundary, and only if the pair passed preflight
   again. Otherwise stay on the recording to the close.
4. Say that the clip is a recording. Do not describe its numbers as live.

## Pre-record before T-6

Record on the operator workstation, one clip per beat, from a single rehearsed
lifecycle on one build. Do not edit out the waits: leave a visible clock, or cut
a wait only with an on-screen “cut: 24 s” caption. Burn in a caption
with the recording date, the commit short hash and the word `RECORDED`.

The tool is a human choice. This workstation has no `asciinema`, `ffmpeg`, OBS
or `wf-recorder` on its path (Verified). It has util-linux `script`, which is
enough for the terminal-only offline clips (Verified, recorded and replayed
`pnpm test:access-flow`):

```sh
script -q --log-timing clip.tm --log-out clip.log -c 'pnpm test:access-flow'
scriptreplay --log-timing clip.tm --log-out clip.log --maxdelay 2
```

Cloud clips with the console need a screen recorder. Choose one and add it to
the rehearsal log.

| Clip | Beat | Budget | Source | Capture |
| --- | --- | --- | --- | --- |
| A | 1 Denied | 0:10–0:25 | Cloud pair | Worker curl, fresh gateway denial line |
| B | 2 Approve | 0:25–0:50 | Cloud pair | Console card, Approve, Sepolia receipt link |
| C | 3 Works and pays | 0:50–1:20 | Cloud pair | Same curl returns 200, current-run HashScan link |
| D | 4 Direct path | 1:20–1:40 | NAT box | Path switches direct, then relay (Open) |
| E | 5 Revoke | 1:40–2:00 | Cloud pair | Revoke click, receipt, fresh denial |
| F | 6 Provider view | 2:00–2:35 | Local | The two capture phases, see below |

Budgets are handoff section 4 slots, not measured durations. Clips A, B, C and
E exceed their slots with the real waits; the three-minute script in
`DEMO_SCRIPT.md` allots 35, 30, 55 and 50 s. Play the recording at the
spoken-script pace and talk over the wait.

### Record clips A, B, C and E

Reserve the pair first. Coordinate with ETH-313, which holds the single live
slot. Read steps and units from `DEMO_SCRIPT.md` and `deploy/DRYRUN.md`; do not
copy secrets onto the screen.

1. Start the nodes at least three minutes early (Read from source):

   ```sh
   ssh bramble-gateway 'sudo systemctl start bramble-demo-service bramble-gateway'
   ssh bramble-worker 'sudo systemctl start bramble-worker'
   ```

2. Run the preflight and reset the grant (Read from source):

   ```sh
   pnpm demo:preflight --role admin --wait 90
   pnpm demo:reset --dry-run
   pnpm demo:reset
   ```

3. Drive one lifecycle and keep its terminal output as the timing record. The
   human supplies `APPROVAL_DEMO_TOKEN` in the environment without displaying it:

   ```sh
   python3 deploy/scripts/rehearse.py --base https://get-bramble.whyash5114.com
   ```

   Record the screen while the harness runs, then repeat once by hand in the
   console so the Approve and Revoke clicks appear in a clip.
4. Confirm the grant is gone and stop the nodes. Save the recording with the
   commit hash, run time and each duration. Mark any missing payment or HCS
   evidence as missing in the clip's caption.

### Record clip F offline

This is Verified (8–12 s wall clock per run, two runs on 6 November 2026). Follow
[beat6-provider-view.md](beat6-provider-view.md) to extract and run the capture.
Show the `direct-local` and `fake-tor-local` phases, the two backend calls and
the absence checks. Caption the clip: “Local fixture, simulated Tor.” Do not
show a before/after of exit addresses from this clip.

For the handoff's real before/after (node IPs, then Tor exits), record a second
clip only after a live run through the public proxy and real Tor. It is Open.

### Record an offline-only substitute for beats 1 and 5

If the cloud pair is unavailable for the whole rehearsal, record these instead
and caption them `LOCAL FIXTURE`:

```sh
pnpm test:access-flow
```

This is Verified (about 2.4 s wall clock, 6 November 2026). It runs the Go gateway test and
the agent MCP flow with in-memory ENS and no Tor. Do not present it as the cloud
demo.

## Timeline

Times are relative to the presentation slot (T).

| When | Action |
| --- | --- |
| T−1 day | Rehearse the lifecycle twice on one build. Record clips A–E from the second run. Record clip F. |
| T−6 h | Open every clip once on the presentation laptop and confirm captions. Copy the folder to a second device. |
| T−30 min | Run `pnpm demo:preflight --role admin --wait 90`. If it fails, plan to start on the recording. |
| T−3 min | Start the nodes and warm beat 1 once (`DEMO_SCRIPT.md`). |
| T | Present live. Switch by the rules above. |

## Rules for the recording

- Show `RECORDED` and the recording date on every frame.
- Do not show a token, header, key file or environment dump.
- Label worker `curl` clips as calls by a Unix user. Only a clip that shows the
  actual Pi session demonstrates an AI agent; use the ETH-243 evidence labels.
- Do not reuse an earlier payment link. Label it with its run time.

## Open items

- A human chooses the recording tool and where to store the files.
- A full recording's availability remains Open until the operator checks it. The live slot is ETH-313.
- Clip D needs a NAT box or the Docker lab; neither has run for this plan.
- The real-Tor before/after for beat 6 is untested.
