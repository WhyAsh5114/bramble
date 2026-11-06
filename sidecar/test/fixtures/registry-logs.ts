// A deterministic stand-in for Sepolia's JSON-RPC, serving LabelRegistered
// logs for the dev tailnet registry. sidecar/test/devices.test.ts uses it so
// `pnpm verify` never depends on public Sepolia. Public nodes prune old logs
// and answer eth_getLogs over this registry's history with error 4444,
// `pruned history unavailable` (Verified against publicnode on 6 November
// 2026, ETH-86).
//
// The logs are built with viem's own encoders from the exact event signature
// sidecar/src/ens/devices.ts decodes. They are not copied from chain, because
// the chain history is what public nodes no longer serve. The transport
// answers only the two methods scanLogsChunked calls and fails loudly on
// anything else, so a change in RPC usage cannot pass silently.
import {
  createPublicClient,
  custom,
  encodeAbiParameters,
  encodeEventTopics,
  keccak256,
  numberToHex,
  parseAbiItem,
  toHex,
  type Address,
  type Hex,
  type PublicClient,
} from 'viem'

const labelRegisteredEvent = parseAbiItem(
  'event LabelRegistered(uint256 indexed tokenId, bytes32 indexed labelHash, string label, address owner, uint64 expiry, address indexed sender)'
)

export interface RawLog {
  address: Address
  topics: Hex[]
  data: Hex
  blockNumber: Hex
  blockHash: Hex
  transactionHash: Hex
  transactionIndex: Hex
  logIndex: Hex
  removed: false
}

export interface GetLogsCall {
  address: string
  topics: (Hex | Hex[] | null)[]
  fromBlock: bigint
  toBlock: bigint
}

const OWNER: Address = '0x000000000000000000000000000000000000b0b0'
const SENDER: Address = '0x000000000000000000000000000000000000a11c'

export function labelRegisteredLog(registry: Address, label: string, blockNumber: bigint, logIndex = 0): RawLog {
  const labelHash = keccak256(toHex(label))
  const topics = encodeEventTopics({
    abi: [labelRegisteredEvent],
    eventName: 'LabelRegistered',
    args: { tokenId: BigInt(labelHash), labelHash, sender: SENDER },
  }) as Hex[]
  const data = encodeAbiParameters(
    [{ type: 'string' }, { type: 'address' }, { type: 'uint64' }],
    [label, OWNER, 2_000_000_000n]
  )
  return {
    address: registry,
    topics,
    data,
    blockNumber: numberToHex(blockNumber),
    blockHash: keccak256(toHex(`block-${blockNumber}`)),
    transactionHash: keccak256(toHex(`tx-${label}-${blockNumber}-${logIndex}`)),
    transactionIndex: '0x0',
    logIndex: numberToHex(logIndex),
    removed: false,
  }
}

// fixtureClient serves `logs` up to chain head `head`. Every eth_getLogs
// request is recorded in `calls` and answered with only the logs inside its
// address and block range, the way a real node does. Set `failWith` to make
// eth_getLogs reject with a JSON-RPC error instead.
export function fixtureClient(opts: { head: bigint; logs: RawLog[]; failWith?: { code: number; message: string } }): {
  client: PublicClient
  calls: GetLogsCall[]
} {
  const calls: GetLogsCall[] = []
  const transport = custom({
    async request({ method, params }: { method: string; params?: unknown }) {
      if (method === 'eth_blockNumber') return numberToHex(opts.head)
      if (method === 'eth_getLogs') {
        const [filter] = params as [{ address: string; topics: (Hex | Hex[] | null)[]; fromBlock: Hex; toBlock: Hex }]
        const call = {
          address: filter.address,
          topics: filter.topics,
          fromBlock: BigInt(filter.fromBlock),
          toBlock: BigInt(filter.toBlock),
        }
        calls.push(call)
        if (opts.failWith) throw Object.assign(new Error(opts.failWith.message), opts.failWith)
        return opts.logs.filter(
          (log) =>
            log.address.toLowerCase() === call.address.toLowerCase() &&
            log.topics[0] === call.topics[0] &&
            BigInt(log.blockNumber) >= call.fromBlock &&
            BigInt(log.blockNumber) <= call.toBlock
        )
      }
      throw new Error(`fixture RPC: unexpected method ${method}`)
    },
  })
  return { client: createPublicClient({ transport }) as PublicClient, calls }
}
