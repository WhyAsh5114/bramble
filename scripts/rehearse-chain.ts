// Rehearses every on-chain write the UI makes, on Sepolia, with the same
// builders (local-ui/src/lib/chain/tx.ts): claim a tailnet, enroll two devices
// with records and a relay list, write an ACL, revoke one device. Prints JSON
// for the Go-side check (brambled/registry live test).
//
// Usage: SEPOLIA_PRIVATE_KEY=... [SEPOLIA_RPC_URL=...] bun scripts/rehearse-chain.ts
// The key is read from the environment and never printed.
import { createPublicClient, createWalletClient, http, type Hex } from 'viem'
import { privateKeyToAccount } from 'viem/accounts'
import { sepolia } from 'viem/chains'
import * as tx from '../local-ui/src/lib/chain/tx'

const NET_REGISTRAR = '0x56a0a5905631e6684bed5dce2e3445af78f6b1c8'
const NET_SUFFIX = 'net.bramble.eth'

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
const randomHex = (n: number) =>
  Array.from(crypto.getRandomValues(new Uint8Array(n)), (b) => b.toString(16).padStart(2, '0')).join('')

const label = `rh${randomHex(3)}`
const claimed = tx.claimedFromReceipt(await send('claim tailnet', tx.claimTailnet(NET_REGISTRAR, label)))
const tailnet: tx.Tailnet = { name: `${label}.${NET_SUFFIX}`, registry: claimed.registry, resolver: claimed.resolver }

const keys = { laptop: randomHex(32), server: randomHex(32) }
await send('register laptop', tx.registerDevice(tailnet, 'laptop', account.address))
await send(
  'laptop records + tailnet relays',
  tx.writeDeviceRecords(tailnet, 'laptop', { pubkey: keys.laptop, meshIp: '10.77.0.1/24', granters: ['laptop'] }, [
    'ap-south-1',
  ])
)
await send('register server', tx.registerDevice(tailnet, 'server', account.address))
await send(
  'server records',
  tx.writeDeviceRecords(tailnet, 'server', { pubkey: keys.server, meshIp: '10.77.0.2/24', granters: ['laptop'] })
)
const digest = randomHex(32)
await send('grant laptop', tx.setACL(tailnet, 'laptop', [digest]))
await send('revoke server', tx.setRevoked(tailnet, 'server', true))

const isAdmin = await pub.readContract({
  address: tailnet.registry,
  abi: tx.registryAbi,
  functionName: 'hasRootRoles',
  args: [tx.ROLE_REGISTRAR, account.address],
})
console.log(JSON.stringify({ tailnet, start: claimed.block.toString(), keys, digest, isAdmin }, null, 2))
