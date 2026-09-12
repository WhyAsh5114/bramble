import type { AbiEvent, Log, PublicClient } from 'viem'

// MAX_BLOCK_RANGE bounds every getLogs window this repo issues. Different
// RPC providers cap eth_getLogs ranges differently and don't advertise the
// limit up front — this repo has now hit two of them for real: publicnode's
// pool enforces something in the ~50,000 range (found live, Sept 12 2026,
// while allocating mesh-ip), and Infura's free tier enforces exactly 10,000
// (found live the same day, immediately after switching to it for
// reliability — both listDeviceLabels and resolveRelays' single
// fromBlock-to-latest getLogs call started failing outright with "range N
// exceeds limit of 10000" the moment the tailnet's own registration history
// grew past that). 9,000 stays under both with margin, and under any other
// common free-tier cap (Alchemy, Ankr, etc. also commonly cap at 10,000) —
// chunking is strictly safer than any single-call range, never slower by
// more than a handful of extra round trips at this project's scale.
const MAX_BLOCK_RANGE = 9_000n

// scanLogsChunked pages a getLogs call in MAX_BLOCK_RANGE windows from
// fromBlock to the chain's current head, concatenating every window's
// results. Exists because a single fromBlock-to-'latest' call — the
// natural way to write "give me everything since this contract was
// deployed" — silently assumes the RPC has no range cap, which no free-tier
// provider this project has tried actually holds true once enough blocks
// have passed. Used by both listDeviceLabels (./devices.ts) and
// resolveRelays (./relays.ts), which had the identical bug independently.
//
// Deliberately loosely typed (decoded args as Record<string, unknown>, not
// viem's fully inferred per-event log shape) — the precise generic
// signature getLogs itself uses doesn't thread cleanly through a wrapper,
// and both callers only ever read `log.args.label`, present on every event
// this project scans regardless.
type ScannedLog = Log & { args: Record<string, unknown> }

export async function scanLogsChunked(
  client: PublicClient,
  params: { address: `0x${string}`; event: AbiEvent; fromBlock: bigint }
): Promise<ScannedLog[]> {
  const head = await client.getBlockNumber()
  const logs: ScannedLog[] = []
  for (let start = params.fromBlock; start <= head; start += MAX_BLOCK_RANGE) {
    const end = start + MAX_BLOCK_RANGE - 1n > head ? head : start + MAX_BLOCK_RANGE - 1n
    const chunk = await client.getLogs({ address: params.address, event: params.event, fromBlock: start, toBlock: end })
    logs.push(...(chunk as ScannedLog[]))
  }
  return logs
}
