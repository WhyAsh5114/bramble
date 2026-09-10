# Gate 0.3 EAC check

Day-0 validation probe, not production code. Run once (Sept 5, 2026) against the live hackathon ENSv2 Sepolia deployment to answer one question: does Enhanced Access Control actually restrict an unauthorized account, or is it decorative? See `docs/10_DAY0_GATES.md` Gate 0.3 for the full writeup and result, and `docs/11_SOURCE_NOTES.md` for the bugs it found along the way (a docs-vs-deployed-bytecode signature mismatch, a CREATE2 address-collision gotcha).

- `index.mjs` — the full 8-step check (register a name, deploy resolvers/subregistry, delegate a scoped EAC role, confirm restriction). Requires `SEPOLIA_PRIVATE_KEY` in `../../.env`.
- `debug-deploy.mjs` — standalone script used to isolate the `UserRegistryImpl.initialize` revert during debugging. Kept as the artifact of that investigation, not needed to reproduce the main result.

Run with `npm install && node index.mjs`.
