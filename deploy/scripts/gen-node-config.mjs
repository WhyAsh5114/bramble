#!/usr/bin/env node
// Build the public node config served at `/.well-known/bramble.json` (the schema
// the `bramble` launcher reads) from env files. Reads only the names in WANTED,
// so a node env file is safe to pass: its private keys are never read. Refuses
// to emit anything that looks like a credential.
// Usage: gen-node-config.mjs --version V --env FILE [--env FILE ...] [--out FILE]
import { readFileSync, writeFileSync } from 'node:fs'
import { pathToFileURL } from 'node:url'

export const WANTED = [
  'BRAMBLE_CONSOLE_HOST',
  'BRAMBLE_RPC_HOST',
  'BRAMBLE_TAILNET_NAME',
  'BRAMBLE_CHAIN_ID',
  'BRAMBLE_TAILNET_REGISTRY',
  'BRAMBLE_RELAY_REGISTRY',
  'BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK',
  'BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK',
  'BRAMBLE_PUBLIC_RPC_URL',
  'BRAMBLE_STUN_SERVERS',
  'BRAMBLE_RENDEZVOUS',
  'BRAMBLE_DATA_RELAYS',
  'BRAMBLE_RELAY_PEERS',
  'BRAMBLE_APPROVAL_URL',
  'BRAMBLE_PUBLIC_SERVICES',
]

export function parseEnv(text) {
  const out = {}
  for (const line of text.split(/\r?\n/)) {
    const m = /^\s*(?:export\s+)?([A-Z][A-Z0-9_]*)=(.*)$/.exec(line)
    if (!m || !WANTED.includes(m[1])) continue
    let v = m[2].trim()
    if (/^(['"]).*\1$/.test(v)) v = v.slice(1, -1)
    if (v !== '') out[m[1]] = v
  }
  return out
}

const list = (v) => (v ? v.split(/[,\s]+/).filter(Boolean) : [])
const need = (env, name) => {
  if (!env[name]) throw new Error(`missing ${name}`)
  return env[name]
}
const address = (env, name) => {
  const v = need(env, name)
  if (!/^0x[0-9a-fA-F]{40}$/.test(v)) throw new Error(`${name} is not a 20-byte hex address`)
  return v
}
const chainId = (env) => {
  const v = need(env, 'BRAMBLE_CHAIN_ID')
  if (!/^[1-9]\d{0,15}$/.test(v)) throw new Error('BRAMBLE_CHAIN_ID is not a chain id')
  return Number(v)
}
const deployBlock = (env, name) => {
  const v = env[name]
  if (!/^\d+$/.test(v) || !Number.isSafeInteger(Number(v))) throw new Error(`${name} is not a nonnegative safe integer`)
  return Number(v)
}
// Same rules as the approval service (approval/src/lib/validation.ts) and the launcher.
const LABEL_RE = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/
const SERVICE_RE = /^[a-z][a-z0-9_-]{0,63}$/
const label = (v, what) => {
  if (!LABEL_RE.test(v)) throw new Error(`${what} "${v}" is not a valid device label`)
  return v
}
const hostPort = (v, what) => {
  if (!/^[A-Za-z0-9.-]+:\d{1,5}$/.test(v)) throw new Error(`${what} entry "${v}" is not host:port`)
  return v
}

// A public URL has no user info, no query and no long opaque path segment
// (the shape of an API key in a provider URL).
export function publicUrl(v, what) {
  let u
  try {
    u = new URL(v)
  } catch {
    throw new Error(`${what} is not a URL`)
  }
  if (u.protocol !== 'https:' && u.protocol !== 'http:') throw new Error(`${what} must be http(s)`)
  if (u.username || u.password) throw new Error(`${what} carries user info`)
  if (u.search || u.hash) throw new Error(`${what} carries a query or fragment`)
  if (u.pathname.split('/').some((s) => /^[A-Za-z0-9_-]{24,}$/.test(s)))
    throw new Error(`${what} has a path segment that looks like an API key`)
  return u.toString()
}

export function buildConfig(env, version) {
  if (!/^[0-9A-Za-z][0-9A-Za-z._-]*$/.test(version ?? ''))
    throw new Error('--version is required (letters, digits, . _ -)')
  const tailnet = need(env, 'BRAMBLE_TAILNET_NAME').toLowerCase()
  if (!/^[a-z0-9-]+(\.[a-z0-9-]+)*\.eth$/.test(tailnet))
    throw new Error('BRAMBLE_TAILNET_NAME is not an ENS name ending in .eth')
  const rpc = env.BRAMBLE_PUBLIC_RPC_URL ?? `https://${need(env, 'BRAMBLE_RPC_HOST')}/`
  const join = env.BRAMBLE_APPROVAL_URL ?? `https://${need(env, 'BRAMBLE_CONSOLE_HOST')}`
  const rendezvous = list(env.BRAMBLE_RENDEZVOUS).map((v) => hostPort(v, 'rendezvous'))
  if (rendezvous.length > 1)
    throw new Error('BRAMBLE_RENDEZVOUS takes one host:port; the node config has one rendezvous field')

  const config = {
    version,
    tailnet,
    chainId: chainId(env),
    rpcUrl: publicUrl(rpc, 'rpcUrl'),
    registry: address(env, 'BRAMBLE_TAILNET_REGISTRY'),
  }
  if (env.BRAMBLE_RELAY_REGISTRY) config.relayRegistry = address(env, 'BRAMBLE_RELAY_REGISTRY')
  if (env.BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK)
    config.registryDeployBlock = deployBlock(env, 'BRAMBLE_TAILNET_REGISTRY_DEPLOY_BLOCK')
  if (env.BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK)
    config.relayDeployBlock = deployBlock(env, 'BRAMBLE_RELAY_REGISTRY_DEPLOY_BLOCK')
  config.joinApi = publicUrl(join, 'joinApi').replace(/\/$/, '')
  if (rendezvous.length) config.rendezvous = rendezvous[0]
  const stun = list(env.BRAMBLE_STUN_SERVERS).map((v) => hostPort(v, 'stun'))
  if (stun.length) config.stun = stun

  // label=sidecar-url, comma-separated
  const relays = {}
  for (const entry of list(env.BRAMBLE_DATA_RELAYS)) {
    const eq = entry.indexOf('=')
    if (eq < 1) throw new Error(`BRAMBLE_DATA_RELAYS entry "${entry}" is not label=url`)
    relays[label(entry.slice(0, eq), 'dataRelays label')] = publicUrl(entry.slice(eq + 1), 'dataRelays').replace(
      /\/$/,
      ''
    )
  }
  if (Object.keys(relays).length) config.dataRelays = relays
  const relayPeers = list(env.BRAMBLE_RELAY_PEERS).map((v) => label(v, 'relayPeers entry'))
  if (relayPeers.length) config.relayPeers = relayPeers

  // peer:service[:/path], comma-separated
  const services = list(env.BRAMBLE_PUBLIC_SERVICES).map((entry) => {
    const [peer, service, path, ...extra] = entry.split(':')
    if (!peer || !service || extra.length)
      throw new Error(`BRAMBLE_PUBLIC_SERVICES entry "${entry}" is not peer:service[:/path]`)
    if (!SERVICE_RE.test(service)) throw new Error(`service name "${service}" is not valid`)
    if (path !== undefined && !path.startsWith('/')) throw new Error(`service path in "${entry}" must start with /`)
    return { peer: label(peer, 'service peer'), service, ...(path ? { path } : {}) }
  })
  if (services.length) config.services = services

  assertNoSecret(JSON.stringify(config))
  return config
}

export function assertNoSecret(text) {
  if (/(^|[^0-9a-fA-F])[0-9a-fA-F]{64}([^0-9a-fA-F]|$)/.test(text))
    throw new Error('output contains a 64-digit hex string (looks like a key)')
  if (/PRIVATE|BEGIN [A-Z ]*KEY|Bearer\s|api[_-]?key/i.test(text))
    throw new Error('output contains a credential-looking word')
}

function main(argv) {
  const files = []
  let out
  let version
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === '--env') files.push(argv[++i])
    else if (argv[i] === '--out') out = argv[++i]
    else if (argv[i] === '--version') version = argv[++i]
    else throw new Error(`unknown argument ${argv[i]}`)
  }
  if (files.length === 0)
    throw new Error('usage: gen-node-config.mjs --version V --env FILE [--env FILE ...] [--out FILE]')
  const env = Object.assign({}, ...files.map((f) => parseEnv(readFileSync(f, 'utf8'))))
  const json = JSON.stringify(buildConfig(env, version), null, 2) + '\n'
  if (out) writeFileSync(out, json)
  else process.stdout.write(json)
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  try {
    main(process.argv.slice(2))
  } catch (e) {
    console.error(`gen-node-config: ${e.message}`)
    process.exit(1)
  }
}
