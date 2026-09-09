// Gate 0.4 (docs/11_DAY0_GATES.md): a trivial HTTP route gated behind a real
// x402 payment, settled through Blocky402's hosted testnet facilitator on
// Hedera testnet. Settlement asset is native HBAR (0.0.0) — deliberately not
// an HTS token, to sidestep the TOKEN_NOT_ASSOCIATED_TO_ACCOUNT trap
// documented in @x402/hedera's own README for this standalone proof. Section
// B (relay metering, not built here) will make its own asset choice.
import "dotenv/config";
import { serve } from "@hono/node-server";
import { Hono } from "hono";
import { paymentMiddleware, x402ResourceServer } from "@x402/hono";
import { HTTPFacilitatorClient } from "@x402/core/server";
import { ExactHederaScheme } from "@x402/hedera/exact/server";
import { HBAR_ASSET_ID } from "@x402/hedera";

const PORT = 4402;
const PRICE_TINYBAR = "1000000"; // 0.01 HBAR

const facilitatorClient = new HTTPFacilitatorClient({ url: "https://api.testnet.blocky402.com" });
const resourceServer = new x402ResourceServer(facilitatorClient).register(
  "hedera:testnet",
  new ExactHederaScheme({
    defaultAssets: { "hedera:testnet": { asset: HBAR_ASSET_ID, decimals: 8 } },
  }),
);

const app = new Hono();

app.use(
  paymentMiddleware(
    {
      "GET /paid-ping": {
        accepts: {
          scheme: "exact",
          network: "hedera:testnet",
          payTo: process.env.HEDERA_SERVER_ACCOUNT_ID,
          price: { asset: HBAR_ASSET_ID, amount: PRICE_TINYBAR },
        },
        description: "Gate 0.4 proof-of-payment ping",
      },
    },
    resourceServer,
  ),
);

app.get("/paid-ping", (c) => c.json({ pong: true, at: new Date().toISOString() }));

console.log(`Gate 0.4 server listening on :${PORT}`);
console.log(`  payTo: ${process.env.HEDERA_SERVER_ACCOUNT_ID}`);
console.log(`  price: ${PRICE_TINYBAR} tinybar (0.01 HBAR)`);
serve({ fetch: app.fetch, port: PORT });
