# How to rehearse the access lifecycle

Run demo beats 1, 2, 3 and 5 with `deploy/scripts/rehearse.py`. The script prints
`RUN`, `PASS`, `FAIL` and elapsed seconds. Exit status is `0` only when every check
passes; failures return `1`, and invalid command arguments return `2`.

1. Check the sequence offline. Python 3 is the only dependency:

   ```sh
   python3 deploy/scripts/rehearse.py --offline
   ```

   This uses in-memory fixtures. Its output starts with `SIMULATED`; it provides
   no evidence of live connectivity, enforcement, payment or chain writes.

2. Coordinate the single live slot through ETH-244 (Infra-Sol-UI). That operator
   prepares and starts the existing pair, checks the initial grant is absent,
   and stops the nodes after the run. Use the setup steps in `DRYRUN.md`.

3. Have the operator supply `APPROVAL_DEMO_TOKEN` in the environment without
   displaying it. The harness uses the approval API; it never loads signer keys.
   Run from the operator workstation with the existing SSH alias:

   ```sh
   python3 deploy/scripts/rehearse.py \
     --base https://console.13-233-181-104.sslip.io
   ```

   This command creates a request, approves it through the configured signer,
   calls the private service, and revokes the agent's grant. It uses the default
   labels `mumbai-worker`, `mumbai-gateway`, and service `task`. It does not manage
   node units or create a cloud slot. The ETH-244 operator owns live execution.

4. Keep the terminal output as the timing record. `beat 5 revoke confirmed`
   measures the approval API transaction wait. `TIMING revoke request to denied`
   measures from the revoke request until a subsequent call is explicitly denied.
   Both include the network and ENS propagation conditions of that run.

5. Stop the nodes using `DRYRUN.md` after the rehearsal. A failed run after an
   approval attempt makes one cleanup revoke attempt. If cleanup is unresolved,
   inspect the grant and transaction state before retrying. A decision write is
   never automatically resubmitted, including after a timeout.

The worker must allow noninteractive SSH and `sudo -n` for `journalctl` and for
`sudo -n -u agent curl`. Host keys must already be trusted. Denial checks require
a fresh worker journal cursor and a matching service's `denied by gateway` line;
a reset, timeout, SSH failure, or unhealthy service alone cannot pass denial.
Run with exclusive use of the worker forward so another caller cannot supply
journal evidence for the same service.

Beat 3 requires HTTP 200 with the demo task's `status: healthy`, a fresh worker
rendezvous-payment HashScan link since the call phase started, and a posted HCS
grant record for this run's request. Payment is for rendezvous; this check does
not claim that the private-service call itself caused a charge. The HCS check
uses the approval recorder's posted receipt and sequence; it does not independently
verify mirror-node inclusion. Missing payment or HCS evidence fails the run and
still proceeds to revoke.

Use `--help` for pair labels, SSH alias, forward URL and timeout overrides.
`--timeout` defaults to 150 seconds per operation; `--wait` defaults to 180 seconds
per propagation or evidence phase, with `--poll 2`. A final operation can extend
past its phase's wait by up to the per-operation timeout. Beat 4 and beat 6 remain
separate checks in `nat-lab/README.md` and `rpc-proxy/README.md`.

Verified: the offline simulation and local fault checks run on 6 November 2026.
Open: ETH-244 must execute the harness against the shared pair and record live
results. No live access or payment claim follows from offline output.
