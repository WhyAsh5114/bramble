// Publishes a relay in the open relay directory on Sepolia with the same
// builders the UI uses (local-ui/src/lib/chain/tx.ts): claim the name, then
// write its records. Any funded Sepolia wallet can do this; no admin role.
//
// Usage: SEPOLIA_PRIVATE_KEY=... bun scripts/publish-relay.ts <label> <host:port> <pin> [--paid] [--stun host:port] [--region r]
// The key is read from the environment and never printed.
import { createPublicClient, createWalletClient, http, type Hex } from 'viem'
import { privateKeyToAccount } from 'viem/accounts'
import { sepolia } from 'viem/chains'
import * as tx from '../local-ui/src/lib/chain/tx'

const raw = (process.env.SEPOLIA_PRIVATE_KEY || process.env.SEPOLIA_PK || '').trim()
if (!raw) throw new Error('set SEPOLIA_PRIVATE_KEY')
const account = privateKeyToAccount((raw.startsWith('0x') ? raw : `0x${raw}`) as Hex)
const transport = http(process.env.SEPOLIA_RPC_URL || 'https://ethereum-sepolia-rpc.publicnode.com', {
  timeout: 60_000,
})
const pub = createPublicClient({ chain: sepolia, transport })
const wallet = createWalletClient({ account, chain: sepolia, transport })

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
async function receipt(hash: Hex) {
  for (let i = 0; i < 150; i++) {
    try {
      return await pub.getTransactionReceipt({ hash })
    } catch {
      await sleep(4000)
    }
  }
  throw new Error(`no receipt for ${hash}`)
}
// eslint-disable-next-line @typescript-eslint/no-explicit-any
async function send(what: string, request: any) {
  const hash = await wallet.writeContract(request)
  const r = await receipt(hash)
  console.error(`${what}: ${hash} (${r.status}, gas ${r.gasUsed})`)
  if (r.status !== 'success') throw new Error(`${what} reverted`)
  return r
}

const RELAY_REGISTRAR = '0x5f42c763e9d3c80a9579ad08bb3c1af0a59752a2'
const RELAY_SUFFIX = 'relay.bramble.eth'

const [label, rendezvous, pin, ...rest] = process.argv.slice(2)
if (!label || !rendezvous || !pin?.startsWith('sha256/')) {
  throw new Error('usage: publish-relay.ts <label> <host:port> <sha256/pin> [--paid] [--stun host:port] [--region r]')
}
if (!tx.LABEL_RE.test(label)) throw new Error(`${label} is not a valid name`)
const opt = (flag: string) => rest[rest.indexOf(flag) + 1]
const paid = rest.includes('--paid')

const claimed = await send('claim', tx.claimRelay(RELAY_REGISTRAR, label))
const { resolver } = tx.claimedFromReceipt(claimed)
await send(
  'write records',
  tx.writeRelayRecords(resolver, RELAY_SUFFIX, label, {
    rendezvous,
    pin,
    dataRelay: true,
    stun: rest.includes('--stun') ? opt('--stun') : undefined,
    region: rest.includes('--region') ? opt('--region') : undefined,
    paid,
  })
)
console.log(JSON.stringify({ name: `${label}.${RELAY_SUFFIX}`, owner: account.address, paid }))
