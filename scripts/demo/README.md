# Demo launcher, preflight and reset

Three commands run the live demo: `pnpm demo` starts what the current machine needs, `pnpm demo:preflight` checks that the demo can run, and `pnpm demo:reset` returns the demo to its initial state on the real chain.

**Evidence labels.** These are the labels in `docs/15_MUMBAI_HANDOFF.md`: **Verified** means someone ran it and saw the result, **Read from source** means taken from code or docs and never run end to end, and **Open** means not tested. The unit tests (`pnpm --filter @bramble/demo-ops test`) are **Verified**. Every run against real machines, the real chain, Hedera, Tor or a STUN server is **needs human verification**. Roles, units, variables, checks and secret handling are listed in [REFERENCE.md](REFERENCE.md).

## How to start the demo

1. On each machine, run the installer from the deploy runbook (`deploy/README.md`, ETH-49). It seeds `/etc/bramble/<role>.env`.
2. Fill in the env files. Set `SEPOLIA_RPC_URL` to a dedicated endpoint if you have one. If it is unset, the default public endpoint works, but reads can occasionally be stale.
3. On the machine you drive from, set how to reach the others (see [Connection settings](REFERENCE.md#connection-settings)). Connect to each remote machine once by hand with ssh, so its host key is known.
4. Run `pnpm demo` on every machine, relays first:

   ```sh
   pnpm demo                   # roles come from /etc/bramble
   pnpm demo --role gateway    # or name them
   pnpm demo --dry-run         # print the plan, start nothing
   ```

   The command starts this machine's systemd units in order, checks that each is active, prints the command to run on the other machines, then runs the preflight and waits up to 120 seconds for Tor to finish starting.

5. Read the preflight report. Every line must be `PASS` or `WARN` before you start the demo.

If a unit fails to start, the launcher stops at that unit, because later units depend on it. It shows the exit code or unit state, never the command's own error output: run `journalctl -u <unit>` for the cause.

## How to run only the checks

```sh
pnpm demo:preflight --role admin --wait 90
```

The command exits `0` when nothing failed (warnings are allowed), `1` when a check failed, and `2` when it could not run. Checks that can pass without help, such as "starting Tor", are retried every 5 seconds for `--wait` seconds.

## How to reset the demo

Run this on the admin machine before each take. It returns the agent device to enrolled, not revoked, with an empty `acl` record (any non-empty record is cleared, even one that holds no digest):

```sh
pnpm demo:reset --dry-run        # read the chain, list what would change, send nothing
pnpm demo:reset                  # send the transactions
```

The reset uses `admincli`'s importable functions (ETH-28), so it sends the same transactions as `admincli revoke-acl` and `admincli revoke`. A second run sends nothing. It does not enrol a device: a device with no `pubkey` is an error. It signs with `SEPOLIA_PRIVATE_KEY`. It loads the granter key from `BRAMBLE_GRANTER_PRIVATE_KEY` or the Key Ring file, and only when a grant needs removing.
