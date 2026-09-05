import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { config } from 'dotenv'
const __dirname = path.dirname(fileURLToPath(import.meta.url))
config({ path: path.resolve(__dirname, '../../.env') })

import {
  createPublicClient, http, parseAbi, encodeAbiParameters, encodeFunctionData,
  keccak256, stringToHex, namehash, getAddress,
} from 'viem'
import { sepolia } from 'viem/chains'
import { privateKeyToAccount } from 'viem/accounts'

const rawKey = process.env.SEPOLIA_PRIVATE_KEY.trim()
const privateKey = rawKey.startsWith('0x') ? rawKey : `0x${rawKey}`
const account = privateKeyToAccount(privateKey)

const VERIFIABLE_FACTORY = getAddress('0x894bc9cc8ff1ad96b8a288c86a8c71d662c07780')
const USER_REGISTRY_IMPL = getAddress('0x47b442d0cf617c41cabaff5f02f44dd1e5f72546')

const publicClient = createPublicClient({ chain: sepolia, transport: http('https://ethereum-sepolia-rpc.publicnode.com') })

const ALL_ROLES = BigInt('0x' + '1'.repeat(64))

const verifiableFactoryAbi = parseAbi([
  'function deployProxy(address implementation, uint256 salt, bytes data) returns (address proxy)',
])
const userRegistryInitAbi = parseAbi([
  'function initialize(address rootAccount, uint256 roleBitmap)',
])

const bigintReplacer = (_k, v) => (typeof v === 'bigint' ? v.toString() : v)
const version = 0n
const fakeName = 'bramble-debug-test.eth'
const salt = BigInt(
  keccak256(
    encodeAbiParameters(
      [{ type: 'bytes32' }, { type: 'bytes32' }, { type: 'uint256' }],
      [keccak256(stringToHex('UserRegistry')), namehash(fakeName), version]
    )
  )
)
const initData = encodeFunctionData({
  abi: userRegistryInitAbi,
  functionName: 'initialize',
  args: [account.address, ALL_ROLES],
})

console.log('Attempting eth_call simulation (no gas spent)...')
try {
  const result = await publicClient.simulateContract({
    account: account.address,
    address: VERIFIABLE_FACTORY,
    abi: verifiableFactoryAbi,
    functionName: 'deployProxy',
    args: [USER_REGISTRY_IMPL, salt, initData],
  })
  console.log('SUCCESS, would deploy to:', result.result)
} catch (e) {
  console.log('--- FULL ERROR ---')
  console.log(JSON.stringify(e, bigintReplacer, 2).slice(0, 4000))
}

// Also try raw eth_call to get raw revert bytes directly
console.log('\nAttempting raw eth_call for raw revert data...')
try {
  const data = encodeFunctionData({
    abi: verifiableFactoryAbi,
    functionName: 'deployProxy',
    args: [USER_REGISTRY_IMPL, salt, initData],
  })
  const res = await publicClient.call({ account: account.address, to: VERIFIABLE_FACTORY, data })
  console.log('raw call result:', res)
} catch (e) {
  console.log('raw call error data:', e.cause?.data || e.data || e.details || e.shortMessage || e.message)
}

// Also try initializing the SAME implementation with a smaller, sane role bitmap
console.log('\nRetrying with a minimal roleBitmap (just ROLE_REGISTRAR | ROLE_RENEW, bit0 of nybble0 and nybble4)...')
const SMALL_ROLES = (1n << 0n) | (1n << 16n) | ((1n << 0n) << 128n) | ((1n << 16n) << 128n)
const initData2 = encodeFunctionData({
  abi: userRegistryInitAbi,
  functionName: 'initialize',
  args: [account.address, SMALL_ROLES],
})
try {
  const result = await publicClient.simulateContract({
    account: account.address,
    address: VERIFIABLE_FACTORY,
    abi: verifiableFactoryAbi,
    functionName: 'deployProxy',
    args: [USER_REGISTRY_IMPL, salt + 1n, initData2],
  })
  console.log('SUCCESS with small roles, would deploy to:', result.result)
} catch (e) {
  console.log('still fails:', e.shortMessage || e.message)
  console.log(JSON.stringify(e, bigintReplacer, 2).slice(0, 3000))
}
