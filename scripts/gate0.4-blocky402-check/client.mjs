// Gate 0.4 client half: calls the paid route once, letting @x402/fetch
// handle the 402 -> sign -> retry loop, and prints the settlement
// transaction id so it can be pasted into HashScan.
import "dotenv/config";
import { wrapFetchWithPaymentFromConfig, decodePaymentResponseHeader } from "@x402/fetch";
import { createClientHederaSigner } from "@x402/hedera";
import { ExactHederaScheme } from "@x402/hedera/exact/client";
import { PrivateKey } from "@hiero-ledger/sdk";

const signer = createClientHederaSigner(
  process.env.HEDERA_CLIENT_ACCOUNT_ID,
  PrivateKey.fromStringECDSA(process.env.HEDERA_CLIENT_PRIVATE_KEY),
  { network: "hedera:testnet" },
);

const fetchWithPayment = wrapFetchWithPaymentFromConfig(fetch, {
  schemes: [{ network: "hedera:testnet", client: new ExactHederaScheme(signer) }],
  // This proof pays in native HBAR, which the client's default spend-control
  // allowlist doesn't recognize as a "default asset" for Hedera yet — disable
  // controls for this standalone script rather than guess at an allowlist shape.
  spendControls: false,
});

const url = "http://localhost:4402/paid-ping";
console.log(`Calling ${url} as ${process.env.HEDERA_CLIENT_ACCOUNT_ID} ...`);

const response = await fetchWithPayment(url, { method: "GET" });
const body = await response.json();
console.log(`HTTP ${response.status}`, body);

const paymentResponseHeader = response.headers.get("PAYMENT-RESPONSE");
if (!paymentResponseHeader) {
  console.error("No PAYMENT-RESPONSE header — payment did not settle. Gate 0.4 not satisfied.");
  process.exit(1);
}

const settlement = decodePaymentResponseHeader(paymentResponseHeader);
console.log("\nSettlement:", settlement);
if (settlement.transaction) {
  console.log(`\nHashScan: https://hashscan.io/testnet/transaction/${settlement.transaction}`);
}
