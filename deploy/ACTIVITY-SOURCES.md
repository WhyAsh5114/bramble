# Configure the console activity sources

Use this procedure for demo beats 1 (gateway denial), 3 (allowed calls and
payments) and 5 (measured cutoff). It configures the existing readers. It does
not authorize a deployment, service restart, new grant, revoke or payment.

1. Arrange an approved private path from the admin process to the **gateway**
   status API, which serves `GET /activity` on loopback port `7899` by default.
   A loopback address on the admin machine reaches the admin machine, not the
   cloud gateway. The existing console reverse tunnel only forwards incoming
   console traffic; it provides no gateway status forward. If the node runs
   inside a network namespace, the path must reach that namespace's loopback
   listener. Do not make statusapi public. Confirm the listener and path with
   the deployment owner before changing a tunnel or node.
2. In the approval service's existing environment file, set `BRAMBLED_STATUS_URL`
   to the base URL reachable through that private path. Do not append `/activity`.
   Set `APPROVAL_ENS_PARENT` to the parent of the demo agent labels so full
   gateway ENS names match approval records. An absent parent can prevent
   cutoff correlation even when the gateway source is live.
3. Set `APPROVAL_PAYMENT_AGENTS` using the worker's public testnet payer ID,
   agent label, advisory budget and a reporting window that has already started.
   Follow [the payment configuration procedure](../docs/16_AGENT_PAYMENTS_FLOW.md#configure-the-reporting-window).
   Choose a window that includes the existing rehearsal settlements being shown.
   Do not copy the sidecar payer's private key to approval. The mirror reader
   needs no payer credential. Match the optional `HEDERA_MAX_PAYMENT_ATOMIC`
   display cap to the sidecar policy (default `100000`).
4. Coordinate an approval-service restart separately. The user unit reads
   `~/.config/bramble/approval.env`; the system unit reads `/etc/bramble/admin.env`.
   Edit the file used by the deployed unit. Preserve the existing token, signer
   settings and ring paths. A restart loses the in-memory request history,
   duration timers and audit openings; do not restart during a pending decision
   or timed grant. Configuring the payment mapping also enables the service's
   existing settlement observer, which can submit HCS commitments if an audit
   trail is configured. Coordinate that effect as part of the deployment.
5. Run this read-only check against the public console. It prints only source
   states and event kinds; it does not send a write or print configuration values.

   ```sh
   python3 - <<'PYTHON'
   import json
   import urllib.request

   url = 'https://console.13-233-181-104.sslip.io/api/activity'
   with urllib.request.urlopen(url, timeout=15) as response:
       feed = json.load(response)
   print(json.dumps({
       'observedAt': feed['observedAt'],
       'sources': feed['sources'],
       'eventKinds': sorted({event['kind'] for event in feed['events']}),
   }, indent=2))
   assert all(feed['sources'].get(source) == 'ok'
              for source in ('approval', 'gateway', 'hedera')), 'activity sources are not ready'
   PYTHON
   ```

   The command exits nonzero for `not_configured` or `unavailable`. Source
   readiness does not prove that every demo event exists. Check existing history
   for `gateway-denied`, `gateway-allowed` and `payment-settled`, and verify the
   settlement link. A measured cutoff additionally needs a retained console
   revoke timestamp and a matching later gateway grant-denial or peer-drop
   event before the next grant. Empty status history, restarts, the wrong ENS
   parent, clock skew and a reporting window excluding the payment can still
   leave evidence absent. Do not create access decisions to validate this task.

The feed retains its explicit health states. A missing source is
`not_configured`; a configured source that fails to read is `unavailable`;
a successful read is `ok`, including an empty source.

See [the public deployment diagnosis](../docs/qa/ETH-254-ACTIVITY-SOURCES.md)
for the observed failure and its limits.
