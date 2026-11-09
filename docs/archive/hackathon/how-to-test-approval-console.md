# Test the approval console in a browser

This procedure checks demo beats 2 and 5 at 1280 and 390 pixels against the real
HTTP API with a test-only in-memory chain. It requires pnpm, Bun and Chromium.
It performs no chain writes. Loopback ports 3001 and 3101 must be free.

1. Install workspace dependencies with `pnpm install --frozen-lockfile`.
2. Install the test browser if needed:
   `pnpm --filter @bramble/approval exec playwright install chromium`.
3. Build the UI with `pnpm --filter @bramble/approval build`.
4. Run `pnpm --filter @bramble/approval test:ui`.
5. Inspect PNG files under `approval/test-results/`. Their names include the
   demo beat, state, viewport width and tested commit SHA.

The harness starts the production UI and an isolated in-memory API proxy. It
feeds the live activity panel from in-memory gateway events and a fake payment,
so the `beat-3-feed-allowed` and `beat-5-feed-cutoff` screenshots do not come
from a real gateway. To read a real `brambled/statusapi` instead, start the
harness with `BRAMBLED_STATUS_URL` set to that node's status URL. The harness
uses `bramble.eth` as the ENS parent, so the node's peer labels must be bare
labels or names under `bramble.eth`.
Screenshots show simulated receipts. They do not prove live testnet writes,
peer cutoff, or public deployment. Upload screenshots with their commit SHA for
H4 human approval. The separate public-URL pass remains required.
