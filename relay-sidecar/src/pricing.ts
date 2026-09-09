// Pure pricing function for the data-plane relay (docs/adr/0008) — kept
// separate from routes/datarelay.ts so it's testable without a running
// server or a real request context.
import { USDC_ASSET_ID, pricePerByteAtomic } from './config'

export type AssetAmount = { asset: string; amount: string }

// priceForBytes computes the atomic USDC charge for a requested byte
// allotment: bytes * pricePerByteAtomic, as an AssetAmount (bypasses
// Money-string USD conversion, same reasoning as the fixed rendezvous price
// in config.ts's PRICE_ATOMIC). Bytes must be a positive integer — callers
// (routes/datarelay.ts) validate the raw query param before this runs.
export function priceForBytes(bytes: number): AssetAmount {
  if (!Number.isInteger(bytes) || bytes <= 0) {
    throw new Error(`bytes must be a positive integer, got ${bytes}`)
  }
  const amount = pricePerByteAtomic() * BigInt(bytes)
  return { asset: USDC_ASSET_ID, amount: amount.toString() }
}
