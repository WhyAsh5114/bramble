// Transaction builders for every on-chain write Bramble makes (ADR 0019).
// Each returns a viem writeContract request; the caller's wallet signs and
// pays. Nothing here holds a key. The UI and scripts/rehearse-chain.ts share
// this module, so the calls the UI makes are the calls the rehearsal tested.
import {
  encodeFunctionData,
  parseAbi,
  parseEventLogs,
  toHex,
  type Address,
  type Hex,
  type TransactionReceipt,
} from 'viem'
import { packetToBytes } from 'viem/ens'

export const openRegistrarAbi = parseAbi([
  'function claimWithSubregistry(string label) returns (address subregistry, address resolver, uint256 tokenId)',
  'function claim(string label) returns (address resolver, uint256 tokenId)',
  'function isValidLabel(string label) view returns (bool)',
  'event Claimed(string label, address indexed owner, address subregistry, address resolver, uint256 tokenId)',
])

export const registryAbi = parseAbi([
  'function register(string label, address owner, address registry, address resolver, uint256 roleBitmap, uint64 expiry) returns (uint256 tokenId)',
  'function getSubregistry(string label) view returns (address)',
  'function getResolver(string label) view returns (address)',
  'function findOwner(string label) view returns (address)',
  'function hasRootRoles(uint256 roleBitmap, address account) view returns (bool)',
])

export const resolverAbi = parseAbi([
  'function setText(bytes name, string key, string value)',
  'function multicall(bytes[] calls) returns (bytes[] results)',
])

/** Text record keys (brambled/registry/tailnet.go). */
export const Keys = {
  pubkey: 'pubkey',
  revoked: 'revoked',
  acl: 'acl',
  aclGranters: 'acl-granters',
  meshIp: 'mesh-ip',
  tailnetRelays: 'relays',
  rendezvous: 'rendezvous',
  pin: 'pin',
  dataRelay: 'data-relay',
  stun: 'stun',
  price: 'price',
  region: 'region',
} as const

/**
 * Gas limits for each write. A wallet estimates against its own RPC node,
 * which can lag behind the block that created the registry and return a far
 * too low estimate (seen in the Sepolia rehearsal: a device registration ran
 * out of gas at 31,835). Only gas used is charged.
 */
// Measured on Sepolia, 9 Nov 2026: claim 2.38M; device register 272k-382k;
// a new text record 150k-375k, growing with the value's length.
export const Gas = {
  claimTailnet: 3_500_000n,
  claimRelay: 2_000_000n,
  register: 700_000n,
} as const

/** Gas limit for writing one text record of this value (new storage, with headroom). */
export function textGas(value: string): bigint {
  return 250_000n + 40_000n * BigInt(Math.ceil(new TextEncoder().encode(value).length / 32))
}

function multicallGas(values: string[]): bigint {
  return values.reduce((sum, v) => sum + textGas(v), 150_000n)
}

/** ENSv2 registry root role that allows registering labels. */
export const ROLE_REGISTRAR = 1n

/** A new device's registration lasts this long; the admin can renew. */
export const DEVICE_TERM_SECONDS = 365n * 24n * 3600n

export interface Tailnet {
  name: string // e.g. alice.net.bramble.eth
  registry: Address
  resolver: Address
}

export const LABEL_RE = /^[a-z0-9](?:[a-z0-9-]{1,30}[a-z0-9])$/

/** DNS wire format of a dot-separated name, as ENS resolvers take it. */
export function dnsName(name: string): Hex {
  return toHex(packetToBytes(name))
}

function setText(name: string, key: string, value: string): Hex {
  return encodeFunctionData({ abi: resolverAbi, functionName: 'setText', args: [dnsName(name), key, value] })
}

/** Claim `label` under the open net registry: deploys the caller's registry and resolver. */
export function claimTailnet(netRegistrar: Address, label: string) {
  return {
    address: netRegistrar,
    abi: openRegistrarAbi,
    functionName: 'claimWithSubregistry',
    args: [label],
    gas: Gas.claimTailnet,
  } as const
}

/** The new tailnet's registry and resolver, from a claim receipt. */
export function claimedFromReceipt(receipt: TransactionReceipt) {
  const [log] = parseEventLogs({ abi: openRegistrarAbi, eventName: 'Claimed', logs: receipt.logs })
  if (!log) throw new Error('the transaction did not claim a name')
  return {
    label: log.args.label,
    owner: log.args.owner,
    registry: log.args.subregistry,
    resolver: log.args.resolver,
    block: receipt.blockNumber,
  }
}

/** Register a device label in the tailnet registry, pointing at the tailnet resolver. */
export function registerDevice(
  tailnet: Tailnet,
  label: string,
  owner: Address,
  nowSeconds = BigInt(Math.floor(Date.now() / 1000))
) {
  return {
    address: tailnet.registry,
    abi: registryAbi,
    functionName: 'register',
    args: [
      label,
      owner,
      '0x0000000000000000000000000000000000000000',
      tailnet.resolver,
      0n,
      nowSeconds + DEVICE_TERM_SECONDS,
    ],
    gas: Gas.register,
  } as const
}

export interface DeviceRecords {
  pubkey: string
  meshIp: string
  /** Device labels whose grants this device accepts as a gateway (ADR 0005). */
  granters: string[]
}

/** Write a device's records, and optionally the tailnet's relay list, in one transaction. */
export function writeDeviceRecords(tailnet: Tailnet, label: string, r: DeviceRecords, tailnetRelays?: string[]) {
  const name = `${label}.${tailnet.name}`
  const values: [string, string, string][] = [
    [name, Keys.pubkey, r.pubkey],
    [name, Keys.meshIp, r.meshIp],
    [name, Keys.aclGranters, r.granters.join(',')],
    [name, Keys.revoked, ''],
  ]
  if (tailnetRelays) values.push([tailnet.name, Keys.tailnetRelays, tailnetRelays.join(',')])
  const calls = values.map(([n, k, v]) => setText(n, k, v))
  return {
    address: tailnet.resolver,
    abi: resolverAbi,
    functionName: 'multicall',
    args: [calls],
    gas: multicallGas(values.map((v) => v[2])),
  } as const
}

/** Revoke (or restore) a device. Peers drop it on their next sync. */
export function setRevoked(tailnet: Tailnet, label: string, revoked: boolean) {
  return {
    address: tailnet.resolver,
    abi: resolverAbi,
    functionName: 'setText',
    args: [dnsName(`${label}.${tailnet.name}`), Keys.revoked, revoked ? 'true' : ''],
    gas: textGas('true'),
  } as const
}

/** Replace a device's ACL digests (each from the granter device's /api/grant-digest). */
export function setACL(tailnet: Tailnet, label: string, digests: string[]) {
  const value = [...new Set(digests)].join(',')
  return {
    address: tailnet.resolver,
    abi: resolverAbi,
    functionName: 'setText',
    args: [dnsName(`${label}.${tailnet.name}`), Keys.acl, value],
    gas: textGas(value),
  } as const
}

/** Replace the tailnet's preferred relay list. */
export function setTailnetRelays(tailnet: Tailnet, labels: string[]) {
  const value = labels.join(',')
  return {
    address: tailnet.resolver,
    abi: resolverAbi,
    functionName: 'setText',
    args: [dnsName(tailnet.name), Keys.tailnetRelays, value],
    gas: textGas(value),
  } as const
}

/** Claim a relay name in the open relay directory. */
export function claimRelay(relayRegistrar: Address, label: string) {
  return {
    address: relayRegistrar,
    abi: openRegistrarAbi,
    functionName: 'claim',
    args: [label],
    gas: Gas.claimRelay,
  } as const
}

export interface RelayRecords {
  rendezvous: string // host:port
  pin: string // sha256/...
  dataRelay: boolean
  stun?: string
  region?: string
}

/** Publish a relay's records on the resolver its claim created. Price is always 0. */
export function writeRelayRecords(resolver: Address, relaySuffix: string, label: string, r: RelayRecords) {
  const name = `${label}.${relaySuffix}`
  const values: [string, string][] = [
    [Keys.rendezvous, r.rendezvous],
    [Keys.pin, r.pin],
    [Keys.dataRelay, r.dataRelay ? 'on' : ''],
    [Keys.stun, r.stun ?? ''],
    [Keys.price, '0'],
    [Keys.region, r.region ?? ''],
  ]
  return {
    address: resolver,
    abi: resolverAbi,
    functionName: 'multicall',
    args: [values.map(([k, v]) => setText(name, k, v))],
    gas: multicallGas(values.map((v) => v[1])),
  } as const
}
