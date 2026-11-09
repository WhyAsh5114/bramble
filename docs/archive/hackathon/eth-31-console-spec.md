# Approval console behavior

ETH-31 implements demo beats 2 and 5 in the separate `approval/` application.
The dashboard remains read-only. The UI uses the dashboard's green and grayscale
palette, mono identity labels, status dots with text, and navigation treatment.

The request queue polls the existing public API every second. Each card shows
agent and gateway labels, freshly resolved identity facts, service, requested
duration, note, and a price estimate field. The current request contract carries
no quote, so this field says “Estimate unavailable.” It must never imply zero
cost. Public identity reads expose only label, published-key status, revocation,
and expiry. They never expose the key itself or upstream diagnostics.

A human enters the shared write token in a password field. The token stays in
React memory for that tab and is cleared on lock or reload. The UI sends it only
in the authorization header of a decision request. The server remains the
authority for authorization, identity validation, state transitions and signing.

Approve and Revoke show a waiting state until the signer confirms. Confirmed
receipts link to Sepolia Etherscan only for a valid transaction hash. The UI
never invents a successful decision. API errors remain visible and polling
continues. A stale queue pauses decisions until a fresh read succeeds.

Durations use the server's remaining-seconds value. Zero seconds means awaiting
revocation, not that enforcement has stopped. Failed automatic revokes explicitly
warn that access may remain active. The server must stay running for its timer.

The task-authorized browser seam is the rendered UI against the real HTTP API
with a test-only in-memory chain. Screenshots at 1280 and 390 pixels cover the
pending request, transaction wait, confirmed grant and confirmed revoke. These
are simulated chain receipts, not evidence of a live network transaction. H4
human screenshot review and the separate public-URL test pass remain required.
