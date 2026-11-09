# How to show an agent payment in demo beat 3

Use the approval console's `/payments` page. The server reads Hedera testnet's
public mirror node. It never signs or forwards a payment. The standalone
`PaymentsPanel` component can also be mounted in the main approval screen.

## Configure the reporting window

1. Give the worker a dedicated Hedera testnet payer. Provision its sidecar with
   `HEDERA_CLIENT_ACCOUNT_ID` and `HEDERA_CLIENT_PRIVATE_KEY` through the existing
   secret injection path. Do not copy the private key into approval configuration.
2. Set `APPROVAL_PAYMENT_AGENTS` on the approval server to a JSON array. Each entry
   contains `agentLabel`, `payerAccountId`, `budgetAtomic`, and `since`. For example,
   the following uses a fixture account, not a deployed demo identity:

   ```json
   [{"agentLabel":"worker","payerAccountId":"0.0.123","budgetAtomic":"100000","since":"1700000000.000000000"}]
   ```

   Replace the account with the worker's public payer ID. Set `since` to the
   rehearsal's start time in Hedera consensus timestamp format (Unix seconds,
   period, nine nanosecond digits). Use a fresh window for each reset, before
   starting the worker's relay connection. An empty or duplicate payer mapping
   fails closed. ENS does not attest this mapping; the operator owns attribution.
   The gateway pays from its own sidecar too. Add a separate gateway label and
   dedicated payer entry to show its spend; the worker's entry excludes it.
3. Set `HEDERA_MAX_PAYMENT_ATOMIC` on approval to the same value used by the
   worker's sidecar. Its default is `100000` (0.100000 USDC per payment). This is
   displayed configuration, not an independent read of the worker's policy;
   the panel labels it as configured and asks the operator to confirm the match.
   `budgetAtomic` is an advisory cumulative reporting budget. It does not stop
   payments. Data-relay price/session limits remain separate node flags.
4. Start the approval server with `pnpm --filter @bramble/approval dev`. Open
   `/payments`. Production uses the approved public console topology; this ticket
   creates no deployment. The feed polls every two seconds after each response.

## Trigger the paid connection inside the flow

1. Before the rehearsal, configure the worker and gateway to use the existing
   metered rendezvous relay registry. Its entries must have `SidecarURL` set, and
   the relay-sidecar must actually challenge with HTTP 402. A bare `-rendezvous`
   TCP address without a payment-sidecar URL does not prove a paid connection.
2. Reset the reporting window, then start the worker's normal `brambled serve`
   connection during the demo. Once the peer's ENS device record is authorized
   (pubkey present, unexpired and not revoked), endpoint resolution calls
   `exchangeAnyMetered`. Each attempt contacts every configured relay concurrently.
   For each metered relay it calls the local sidecar's `/rendezvous-token`, buys
   a fresh relay-specific token over x402 **before dialing**, and presents it in
   the rendezvous hello. Both nodes exchange, each using its own sidecar/payer.
   This device admission is separate from service-ACL approval, so payment can
   precede Approve. A failed exchange leaves the endpoint unresolved and buys
   tokens again on the next admission sync (`-ttl`, default 30 seconds). A
   successful endpoint resolution is remembered for that pubkey. Retrying the
   private service call alone does not buy another token.
3. Run the denied → request → human approve → successful private call flow with
   the same running worker. At beat 3 show `/payments`, including the payer,
   configured per-payment cap, spent, remaining advisory budget, and HashScan settlement.
   The settlement belongs to the relay connection made inside this demo window;
   it is not a fee charged by the gateway for the successful service call.
4. For the explicit data-relay variant, configure the existing `-data-relay
   <label>=<relay-sidecar-url>` and `-relay-peer <peer-label>` flags before starting
   the worker. The node quotes candidates and applies `-max-relay-price-per-byte`
   and `-max-relay-session-cost`, then buys a session before routing that peer.
   The session cost must also fit under the sidecar's `HEDERA_MAX_PAYMENT_ATOMIC`.
   This may buy both a session and rendezvous tokens. Failover buys another
   session; an idle relay stall may also cause a purchase.

   With a data-relay pool and rendezvous configured, each tracked peer not in
   this node's `-relay-peer` set also runs `watchDataRelayAdopt`. It buys a fresh
   token from **every metered rendezvous relay on every cycle**, even when the
   candidate does not change. The exchange usually gives a roughly 3-second
   cycle, rather than a guaranteed purchase interval. Backoff starts only after
   two minutes without a change and adds a delay that doubles up to 30 seconds.
   At the ADR 0007 default of 10000 atomic USDC per token, the example advisory
   budget of 100000 covers only ten tokens: roughly 30 seconds for one relay in
   that loop, sooner with multiple relays or other purchases. Stop the demo nodes
   at the end of the take; reaching the displayed budget does not stop the loop.
5. Compare the worker's logged settlement ID with the panel's HashScan record.
   Measure settlement-to-panel latency; the two-second poll interval is not a
   guarantee of mirror indexing or Tor payment latency. If no new outgoing USDC
   appears, check whether the connection predates `since` or uses an unmetered
   relay. Do not manufacture a payment by clicking Approve repeatedly.

See [the payment feed reference](17_AGENT_PAYMENTS_REFERENCE.md) for evidence,
feed limits and remaining human verification gates.
