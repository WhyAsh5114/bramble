# Node release reference

The device bundle serves demo beats 2 and 3 (device onboarding and the first call through the tailnet). It contains three executables and requires no Go, Bun, Node.js or pnpm on the device.

## Build interface

`scripts/release.sh [os/arch ...]` runs on Linux with Go, Bun, Node.js, pnpm, GNU tar, gzip and sha256sum. Workspace dependencies must be installed with `pnpm install --frozen-lockfile`. The script builds the local web UI (`pnpm --filter @bramble/local-ui build`), copies it into `node-daemon/ui/dist`, then compiles each target.

| Input | Default | Meaning |
| --- | --- | --- |
| Positional targets | `linux/amd64 linux/arm64 darwin/arm64 darwin/amd64` | Targets to compile and archive |
| `VERSION` | `git describe --tags --always --dirty` | Build log label; also the output of `bramble version` |
| `BRAMBLE_SKIP_UI` | unset | Set to `1` to skip the UI build and embed the existing `node-daemon/ui/dist` |

`bramble` is the node daemon from `node-daemon/` (ETH-180). It serves the embedded local UI and the control API on `127.0.0.1`, runs the join flow and supervises `brambled serve`. `brambled` is the helper from `brambled/`, and the daemon finds it next to its own executable. The script stops if `node-daemon/ui/dist/index.html` is missing, because the daemon would otherwise serve its placeholder page.

## Artifacts

`release/` is gitignored. Each `bramble_<os>_<arch>.tar.gz` contains executable files `bramble`, `brambled` and `bramble-sidecar` at its root. `SHA256SUMS` contains checksums for the archives in that directory. Go uses `CGO_ENABLED=0`; Bun uses `bun build --compile`. Archive metadata uses the commit timestamp, numeric owner/group 0 and gzip without a timestamp.

Only the host target executes the signing self-test during the build. Cross-compiled targets require a compatible host for runtime validation. The build does not claim byte-for-byte binary reproducibility across toolchain versions.

## Sidecar selection

`brambled` chooses its child process in this order:

1. `BRAMBLE_SIDECAR_BIN`: an executable path, absolute or relative to the current directory. A missing or non-executable override fails startup.
2. `bramble-sidecar` beside the daemon executable. The daemon resolves its own symlinks first.
3. Development fallback: `bun run src/index.ts` in `BRAMBLE_SIDECAR_DIR` (default `../sidecar`).

The compiled sidecar embeds the Tor WASM through `tor-js/wasm-base64`. Tor uses `MemoryStorage`; bootstrap needs network access but no downloaded WASM or disk cache. Data routes stay gated until Tor is ready, as specified in ADR 0013.

## Validation interface

`bramble-sidecar --self-test` generates a throwaway Hedera ECDSA key, creates an x402 transfer payload, decodes it and verifies its signature and debit amount. It makes no network request, spends no funds and needs no configured key.

`node scripts/release-smoke.mjs [archive]` is a host-side harness; Node.js is not part of the device bundle. It validates the archive checksum, extracts the bundle into a temporary directory and runs them with `PATH=/nonexistent`, an empty home and an explicit environment without inherited credentials. It checks the compiled signing self-test, then runs `brambled resolve device1` against the public Sepolia fixture through real Tor. It asserts the expected ENS fullname, public key and active status, and checks that the runtime writes no files in its home or working directory. It removes the temporary directory afterward. A run is bounded to eight minutes per command. The default archive is `release/bramble_linux_amd64.tar.gz`.

## Recorded evidence

**Verified, 6 November 2026, Linux amd64:** the stripped-PATH harness passed against the public Sepolia RPC. The compiled Tor client became ready after 3.7 seconds. The daemon found its sibling sidecar and resolved `device1.bramble-dev-c91e55e9.eth` with status `2` and the fixture public key `3aecd3d39c7c878e64f48258e41548e37fe5cf97a4edc53511ebe71170b77360`. The signing self-test passed, and no runtime cache or assets appeared in the temporary home or working directory. This is a stripped-PATH test on the build host, not a separate container or an isolated filesystem.

**Open:** runtime execution on Linux arm64 and macOS, and live payment settlement from a compiled binary. The self-test verifies signing without settlement.
