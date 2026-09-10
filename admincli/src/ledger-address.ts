// The Ledger device's own Ethereum address — needed only as a plain
// function-call argument (e.g. the `owner` passed to register(), or a
// roleBitmap grant target), never for signing. Signing goes through
// ledger-send.ts's sendViaDevice, which addresses the device by account
// *label*, not this value; this is public info that just happens to be
// discovered the same way.
import { getAddress, type Address } from 'viem'

export function ledgerAddressFromEnv(): Address {
  const raw = process.env.BRAMBLE_LEDGER_ADDRESS?.trim()
  if (!raw) {
    throw new Error(
      'BRAMBLE_LEDGER_ADDRESS not set — run `wallet-cli account discover` once with the device ' +
        'connected, then set this to the address it prints'
    )
  }
  return getAddress(raw)
}
