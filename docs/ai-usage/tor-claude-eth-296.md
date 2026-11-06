# Tor-Claude — ETH-296

Beat 6 provider-view capture and the no-cloud fallback plan. Demo beat: 6 (and the recorded fallback in handoff section 4). The first draft of `beat6-provider-view.md` came from a previous agent in this worktree; Tor-Claude re-ran its capture and edited it.

Paths written:

- `docs/demo/beat6-provider-view.md`
- `docs/demo/fallback.md`
- `docs/ai-usage/tor-claude-eth-296.md`

Verified (6 November 2026, base `52a35cb`): the beat 6 capture (49 `eth_call` requests per phase, two backend calls, 8–12 s wall clock; decoded params recover device labels and record keys), `pnpm test:access-flow` and `python3 deploy/scripts/rehearse.py --offline`. No cloud node was used.

ETH-306 (CI repair, no new files): `verify` on `c720c12` failed in `brambled#test` (`TestDataRelayFailover_SwitchesToBackupRelayWhenPrimaryStalls`, a timing-sensitive Go relay test; the PR changes only docs). Merged `origin/hackathon` into the branch and re-ran `pnpm verify --force` locally (green on the second run; the first run hit a different flaky relay test, `TestSponsoredRelay_BootstrapKeepsSessionUntilPeerHandshakes`, while sidecar tests ran in parallel). Paths written: this file only.
