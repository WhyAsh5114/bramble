// The wallet writes that make a device a member: register its name, then
// write its key, mesh address and trusted granters in one multicall.
import type { Address, TransactionReceipt } from 'viem'
import * as tx from './chain/tx'

export interface EnrollInput {
  tailnet: tx.Tailnet
  label: string
  owner: Address
  pubkey: string
  meshIp: string
  granters: string[]
  /** Write the tailnet's preferred relay list in the same transaction. */
  relays?: string[]
  /** The name is already registered (an earlier attempt stopped half way, or a re-key). */
  registered: boolean
}

type Send = (request: Record<string, unknown>) => Promise<TransactionReceipt>

export function enrollSteps(send: Send, e: EnrollInput) {
  const steps: { label: string; run: () => Promise<TransactionReceipt> }[] = []
  if (!e.registered)
    steps.push({
      label: `Register ${e.label}.${e.tailnet.name}`,
      run: () => send(tx.registerDevice(e.tailnet, e.label, e.owner)),
    })
  steps.push({
    label: `Write ${e.label}'s key and address${e.relays ? ' and the tailnet relay list' : ''}`,
    run: () =>
      send(
        tx.writeDeviceRecords(
          e.tailnet,
          e.label,
          { pubkey: e.pubkey, meshIp: e.meshIp, granters: e.granters },
          e.relays
        )
      ),
  })
  return steps
}
