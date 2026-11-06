import { test } from 'node:test'
import assert from 'node:assert/strict'
import { parseEnv, buildConfig } from './gen-node-config.mjs'

const ADDR_A = '0x' + 'a'.repeat(40)
const ADDR_B = '0x' + 'b'.repeat(40)
const base = `
BRAMBLE_CONSOLE_HOST=console.example.com
BRAMBLE_RPC_HOST=rpc.example.com
BRAMBLE_TAILNET_NAME=Demo-Net.eth
BRAMBLE_CHAIN_ID=11155111
BRAMBLE_TAILNET_REGISTRY=${ADDR_A}
BRAMBLE_RELAY_REGISTRY=${ADDR_B}
BRAMBLE_STUN_SERVERS=203.0.113.5:3478
BRAMBLE_RENDEZVOUS=203.0.113.5:9420
BRAMBLE_DATA_RELAYS=relay-a=http://203.0.113.5:7891
`
const build = (text, version = '0.1.0') => buildConfig(parseEnv(text), version)

test('builds the launcher schema with defaults derived from the host names', () => {
  assert.deepEqual(build(base), {
    version: '0.1.0',
    tailnet: 'demo-net.eth',
    chainId: 11155111,
    rpcUrl: 'https://rpc.example.com/',
    registry: ADDR_A,
    relayRegistry: ADDR_B,
    joinApi: 'https://console.example.com',
    rendezvous: '203.0.113.5:9420',
    stun: ['203.0.113.5:3478'],
    dataRelays: { 'relay-a': 'http://203.0.113.5:7891' },
  })
})

test('preserves registry deployment blocks for fresh device and relay discovery', () => {
  const c = build(
    `${base}\nBRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK=12074600\nBRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK=12074600`
  )
  assert.equal(c.registryDeployBlock, 12074600)
  assert.equal(c.relayDeployBlock, 12074600)
})

test('rejects deployment blocks that cannot be represented as exact nonnegative integers', () => {
  for (const name of ['BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK', 'BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK']) {
    for (const value of ['-1', '1.5', 'abc', '1e7', '0x123', '9007199254740992']) {
      assert.throws(() => build(`${base}\n${name}=${value}`), new RegExp(name))
    }
  }
})

test('omits optional fields that are not set', () => {
  const c = build(base.replace(/BRAMBLE_(RELAY_REGISTRY|DATA_RELAYS|STUN_SERVERS|RENDEZVOUS)=.*/g, ''))
  for (const k of [
    'relayRegistry',
    'dataRelays',
    'stun',
    'rendezvous',
    'relayPeers',
    'services',
    'registryDeployBlock',
    'relayDeployBlock',
  ])
    assert.ok(!(k in c), k)
})

test('parses relay peers and services', () => {
  const c = build(
    `${base}\nBRAMBLE_RELAY_PEERS=worker,gateway\nBRAMBLE_PUBLIC_SERVICES=worker:task:/health,gateway:api`
  )
  assert.deepEqual(c.relayPeers, ['worker', 'gateway'])
  assert.deepEqual(c.services, [
    { peer: 'worker', service: 'task', path: '/health' },
    { peer: 'gateway', service: 'api' },
  ])
})

test('never reads secrets from a node env file', () => {
  const secret = 'f'.repeat(64)
  const env = parseEnv(
    `${base}\nBRAMBLE_PRIVATE_KEY=${secret}\nSEPOLIA_RPC_URL=https://x.example/v3/abcdefabcdefabcdefabcdef12\nHEDERA_CLIENT_PRIVATE_KEY=302e0201\n`
  )
  assert.deepEqual(Object.keys(env).sort(), Object.keys(parseEnv(base)).sort())
  assert.ok(!JSON.stringify(buildConfig(env, '0.1.0')).includes(secret))
})

test('rejects a credentialed RPC URL', () => {
  for (const url of [
    'https://user:pw@rpc.example.com/',
    'https://rpc.example.com/?apikey=1',
    'https://rpc.example.com/v3/abcdefabcdefabcdefabcdef12',
  ]) {
    assert.throws(() => build(`${base}\nBRAMBLE_PUBLIC_RPC_URL=${url}`), /rpcUrl/)
  }
})

test('rejects missing and malformed values', () => {
  assert.throws(() => build(base.replace(/BRAMBLE_TAILNET_NAME=.*/, '')), /BRAMBLE_TAILNET_NAME/)
  assert.throws(() => build(base.replace(/BRAMBLE_CHAIN_ID=.*/, '')), /BRAMBLE_CHAIN_ID/)
  assert.throws(() => build(base.replace('11155111', 'sepolia')), /chain id/)
  assert.throws(() => build(base.replace(ADDR_A, '0x12')), /address/)
  assert.throws(() => build(base.replace('203.0.113.5:3478', 'nohost')), /host:port/)
  assert.throws(() => build(base.replace('Demo-Net.eth', 'demo.com')), /\.eth/)
  assert.throws(() => build(base, ''), /--version/)
  assert.throws(() => build(base.replace('relay-a=', '')), /label=url/)
  assert.throws(() => build(base.replace('relay-a=', 'Bad_Label=')), /label/)
  assert.throws(() => build(`${base}\nBRAMBLE_PUBLIC_SERVICES=worker:task:health`), /start with/)
  assert.throws(() => build(`${base}\nBRAMBLE_RENDEZVOUS=203.0.113.5:9420,203.0.113.6:9420`), /one host:port/)
})

test('rejects a 64-digit hex string anywhere in the output', () => {
  const key = '1'.repeat(64)
  assert.throws(() => build(`${base}\nBRAMBLE_PUBLIC_SERVICES=worker:task:/${key}`), /looks like a key/)
})
