// A from-scratch Ethereum transaction signer against the Ledger device
// itself, via @ledgerhq/hw-app-eth directly — NOT wallet-cli. wallet-cli's
// `send` doesn't support Sepolia (or any testnet/L2) as of v2.1.0, confirmed
// against Ledger's own agent-skills repo (docs/12_LEDGER_DX_FEEDBACK.md, Gap
// 4) — this module is the workaround, built and verified against real
// hardware Sept 11, 2026 (a real EIP-1559 self-send, signed, recovered
// locally, and confirmed on Sepolia before this was ever wired into
// enroll.ts).
//
// Deliberately not used by set-acl.ts: that script needs wallet-cli's
// Ledger Sync app (for `ring decrypt`) and this module needs the Ethereum
// app — both open in the same run isn't possible, and set-acl.ts's device
// story (headless Key Ring decrypt) is already verified and doesn't need
// per-transaction signing anyway. This is for enroll.ts only, where "press
// the button to admit a new device" is a literal, real requirement, not a
// retrofit.
//
// Two Ledger-specific gotchas, discovered building this, not assumed from
// docs — see docs/12_LEDGER_DX_FEEDBACK.md for the DX writeup:
// - `signTransaction`'s returned `v` for an EIP-1559 (type 2) transaction is
//   the yParity (0 or 1), not an EIP-155-encoded v. Passing it straight
//   through as `v` produces a signature that serializes fine but recovers
//   to a wrong address — caught locally (see verifyAndSerialize below), not
//   on-chain, only because we check for it up front.
// - The resolution argument must be passed as `null` to sign as a blind,
//   unrecognized transaction (this repo's calldata — setText/register —
//   isn't a known ERC-20/ERC-721 method Ledger's CAL service would
//   recognize anyway). Blind signing must be enabled in the Ethereum app's
//   own settings on-device, or the device silently refuses.
import { toAccount } from 'viem/accounts'
import { serializeTransaction, recoverTransactionAddress, type TransactionSerializable } from 'viem'

const DERIVATION_PATH = "44'/60'/0'/0/0"

interface LedgerEthSignature {
  v: string
  r: string
  s: string
}

// Structural typing only (avoids depending on @ledgerhq/hw-app-eth's own
// type exports, which vary across its major versions) — the two methods
// this module actually calls.
interface LedgerEthLike {
  getAddress(path: string): Promise<{ address: string }>
  signTransaction(path: string, rawTxHex: string, resolution: null): Promise<LedgerEthSignature>
}

function yParityFromDeviceV(v: string): 0 | 1 {
  const n = parseInt(v, 16)
  if (n === 0 || n === 1) return n
  // Legacy 27/28 encoding, seen on some hw-app-eth versions even for type-2
  // transactions — normalize the same way.
  return (n % 2 === 0 ? 1 : 0) as 0 | 1
}

async function verifyAndSerialize(
  address: `0x${string}`,
  transaction: TransactionSerializable,
  sig: LedgerEthSignature
): Promise<`0x${string}`> {
  const signature = {
    r: `0x${sig.r}` as `0x${string}`,
    s: `0x${sig.s}` as `0x${string}`,
    yParity: yParityFromDeviceV(sig.v),
  }
  const serialized = serializeTransaction(transaction, signature)
  const recovered = await recoverTransactionAddress({ serializedTransaction: serialized })
  if (recovered.toLowerCase() !== address.toLowerCase()) {
    throw new Error(
      `Ledger signature recovers to ${recovered}, not ${address} — yParity or signature construction is wrong; not broadcasting`
    )
  }
  return serialized
}

// Opens a real USB connection to the device and returns a viem-compatible
// local account whose signTransaction goes to the Ledger's Ethereum app,
// plus a close() to release the transport when done. Caller must always
// close(), success or failure (see enroll.ts's try/finally).
export async function connectLedgerAccount() {
  // Native module, CJS-only exports (their ESM build has a broken exports
  // map in a nested dependency as of the versions in package.json) — import
  // via createRequire rather than a static `import`, which would resolve
  // through the broken ESM path and fail before this function ever runs.
  const { createRequire } = await import('node:module')
  const require = createRequire(import.meta.url)
  const TransportNodeHid = require('@ledgerhq/hw-transport-node-hid').default
  const Eth = require('@ledgerhq/hw-app-eth').default

  const transport = await TransportNodeHid.create()
  const eth: LedgerEthLike = new Eth(transport)
  const { address } = await eth.getAddress(DERIVATION_PATH)

  const account = toAccount({
    address: address as `0x${string}`,
    async signMessage() {
      throw new Error('signMessage not implemented for the Ledger account — only signTransaction is needed here')
    },
    async signTypedData() {
      throw new Error('signTypedData not implemented for the Ledger account — only signTransaction is needed here')
    },
    async signTransaction(transaction) {
      const serializedUnsigned = serializeTransaction(transaction)
      const rawTxHex = serializedUnsigned.slice(2)
      console.log('   >> confirm this transaction on your Ledger now...')
      const sig = await eth.signTransaction(DERIVATION_PATH, rawTxHex, null)
      return verifyAndSerialize(address as `0x${string}`, transaction, sig)
    },
  })

  return {
    account,
    close: () => transport.close(),
  }
}
