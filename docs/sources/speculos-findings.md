# Speculos validation report

**Date:** Sept 8, 2026 (IST). Scratch-only investigation — no repo code was changed.
**Question:** Can the Ledger demo flow run under Speculos (no physical device)?

## One-line answer

The **device-confirmed EVM signing path** (`wallet-cli send`'s job) works under
Speculos with real signatures. The **Key Ring path** (`wallet-cli ring init`)
does not — and cannot — because it requires a real secure element plus Ledger's
trustchain attestation.

---

## 1. What was set up

- Docker image: `ghcr.io/ledgerhq/speculos:latest`
- App: **Ethereum 1.22.3** (`app-1.22.3-nanos2.elf`, real app binary, not a stub)
  - From `https://github.com/LedgerHQ/app-ethereum/releases/download/1.22.3/app-1.22.3-nanos2.elf`
- Model: `nanosp` (Nano S+)
- Test seed (safe to publish, emulator-only):
  `glory promote mansion idle axis finger extra february uncover one trip resource lawn turtle enact monster seven myth punch hobby comfort wild raise skin`

```bash
curl -sL -o /tmp/speculos/ethereum-nanos2.elf \
  https://github.com/LedgerHQ/app-ethereum/releases/download/1.22.3/app-1.22.3-nanos2.elf

docker run -d --name speculos -p 5001:5000 -p 9999:9999 -p 41000:41000 \
  -v /tmp/speculos:/speculos/apps ghcr.io/ledgerhq/speculos:latest \
  --model nanosp \
  --seed "glory promote mansion idle axis finger extra february uncover one trip resource lawn turtle enact monster seven myth punch hobby comfort wild raise skin" \
  --display headless --api-port 5000 --apdu-port 9999 --vnc-port 41000 \
  /speculos/apps/ethereum-nanos2.elf
```

### macOS gotcha (bit the earlier attempt)

Port **5000 is owned by macOS AirPlay** (`ControlCe` / AirTunes). Speculos's
REST API is on container port 5000, so it must be mapped to a different host
port. Everything below uses host `5001` → container `5000`.

- APDU (raw) server: container `9999` → host `9999`
- REST API (buttons, screens, events): container `5000` → host `5001`
- VNC (screen viewer): `41000`

---

## 2. What works — device-confirmed signing (`send` path)

Signing EVM transactions under Speculos uses the LedgerJS transport/API stack,
not `wallet-cli` itself:

- `@ledgerhq/hw-transport-node-speculos@6.34.7` (talks APDU over TCP)
- `@ledgerhq/hw-app-eth@7.8.17` (Ethereum app API)
- Speculos REST button API for the on-device review flow

Derived device address (path `44'/60'/0'/0/0`):

```
0xDad77910DbDFdE764fC21FCD4E74D71bBACA6D8D
```

### Result

Signed a **real EIP-1559 contract call** on Sepolia chain id — a `setText(bytes,
string, string)` calldata transaction, the same shape enroll/revoke sends to the
ENSv2 registry:

```
device-address:    0xDad77910DbDFdE764fC21FCD4E74D71bBACA6D8D
signature:         {"v":"00","r":"8b496bf05fb56626b000f5c3fca0fee1f9ea2318cabdfd3680c1c5810329d23b","s":"704d6184e522d98e3f3122335425de3eb1b67aa2a96bc8dbe04781dc2e3aea13"}
recovered-signer:  0xDad77910DbDFdE764fC21FCD4E74D71bBACA6D8D
match:             YES
calldata-preserved: YES
```

- Signature **recovery matches the device address** (verified with ethers).
- Calldata round-trips byte-for-byte.
- Deterministic: same seed + same tx → same signature (no randomness leaks).

### Required on-device setting: Blind signing

Contract data is rejected unless **Blind signing** is enabled in the Ethereum
app settings, otherwise:

```
EthAppPleaseEnableContractData: Please enable Blind signing or Contract data
in the Ethereum app Settings
```

Enable it once after boot (main menu → `App settings` → `Blind signing` →
toggle to `Enabled` → `Back`). This is a plain on-device setting, present on
real devices too.

### Review flow (Nano S+, contract call)

The button driver must handle two final screens — the plain "Sign transaction"
for transfers and "Accept risk and sign transaction" for blind-signed contract
calls:

```
Blind signing ahead  To accept risk, press  both buttons   -> BOTH
Review transaction                                          -> right
From 0xDad77910…                                            -> right
To 0x0000…dead                                              -> right
Max fees 0.00024 ETH                                        -> right
Network Sepolia                                             -> right
Tx hash (1/2) …                                             -> right
Tx hash (2/2) …                                             -> right
Accept risk and  sign transaction                           -> BOTH
Transaction signed
```

### Working validation script (scratch)

`/tmp/speculos-test/validate-v2.cjs` — opens the APDU transport, enables Blind
signing, signs a calldata transaction, drives the review flow over the REST
button API, and verifies the recovered signer. Dependencies installed in
`/tmp/speculos-test/node_modules`.

---

## 3. What does NOT work — `ring init` (Key Ring)

Confirmed with fresh receipts, not just the prior ADR:

### `wallet-cli` cannot see Speculos at all

`wallet-cli` v2.1.0 is USB-only. With `SPECULOS_*` env vars set and Speculos
running, both fail identically:

```
wallet-cli genuine-check   -> [✖] No Ledger device found. Unlock the device and try again.
```

The `wallet-cli` README states this explicitly: it is "the stable CLI for
USB-based Ledger Wallet flows," and the Speculos-capable tool is
`@ledgerhq/live-cli` (`apps/cli`), an internal Ledger Live test tool.

### `MOCK=1` does not remove the device requirement

```
$ MOCK=1 WALLET_PASS=test wallet-cli ring init --unsecure-no-password
Generating member credentials…
Connect device, open Ledger Sync app — provisioning your Ledger Key Ring…
[✖] No Ledger device found.
```

Mock mode only swaps the *backend*; `ring init` still requires the **Ledger
Sync** app on a device to derive the encryption key.

### The LKRP "mock SDK" is a test double, not the Key Ring

`@ledgerhq/ledger-key-ring-protocol@0.15.2` ships a `MockSDK`
(`getSdk(isMockEnv=true)`). Its internals:

```js
// to mock the encryption/decryption, we just xor the data with 0xff
rootId: "mock-root-id",
walletSyncEncryptionKey: "mock-wallet-sync-encryption-key",
privatekey: "mock-private-key-…",
```

That is XOR-with-0xFF, mock keys, and a mock trustchain. It is not
hardware-rooted crypto, and would be an immediate misrepresentation to a Ledger
judge. Not usable for the demo.

### The staging trustchain is reachable but attestation-gated

Found in the compiled binary:

- `TRUSTCHAIN_API_STAGING` = `https://trustchain-backend.api.aws.stg.ldg-tech.com`
- `TRUSTCHAIN_API_PROD`   = `https://trustchain.api.live.ledger.com`

Both answer HTTP (JSON 404 `"No such endpoint, please check API docs"`), so the
staging backend is publicly reachable. However its auth flow requires a
**secure-element attestation** — the challenge-signature carries an
`attestation` field. Speculos can emit *test* attestation via
`--attestation-key`, but only a backend configured to trust that specific test
key accepts it, and that trust configuration is Ledger-internal.

Also: there is **no prebuilt `app-ledger-sync` ELF** (the Ledger Sync app repo
has no releases), so the sync app would have to be built from source first.

### Why the theoretical path is a dead end

Even a determined attempt would need all of:

1. Patch `wallet-cli` from the `LedgerHQ/ledger-live` monorepo to wire in the
   speculos transport (DMK exposes `speculosTransportFactory`, but the shipped
   binary doesn't call it).
2. Build the **Ledger Sync** app from source (no prebuilt ELF).
3. Run Speculos with a test attestation key the staging backend trusts.
4. Point the flow at the staging trustchain.

Items 1–2 are heavy but possible. Item 3 requires a Ledger-internal test key /
trust configuration that is not public. And none of it produces a production
attestation — it would only work against staging, not the real trustchain a
real device uses.

---

## 4. Bottom line

| Flow | Under Speculos | Notes |
|---|---|---|
| Device-confirmed EVM signing (`send` / enroll-revoke) | ✅ works | Real Ethereum-app signatures; recovery verified |
| `ring init` (Key Ring provisioning) | ❌ blocked | Needs real secure element + production attestation |
| `ring encrypt` / `decrypt` after a real `init` | (not tested) | Depends on a real `init` first |

Speculos gives a **real-signature stand-in for the enroll/revoke
device-confirmation beat** (Gate 3.1's signing half) while the physical Nano S
Plus is in transit. It does **not** unblock Gate 0.1 or the flagship
"`wallet-cli ring` on a headless host" claim — that still requires the physical
device, exactly as documented in `adr/0001-ledger-ring-vs-send-split.md`.
