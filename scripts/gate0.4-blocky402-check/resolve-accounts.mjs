// Gate 0.4 step 1 (continued): after funding both EVM addresses via
// https://portal.hedera.com/faucet, look up the resulting 0.0.X account IDs
// from the public testnet mirror node and write them into .env.
import 'dotenv/config'
import { readFileSync, writeFileSync } from 'node:fs'

const MIRROR = 'https://testnet.mirrornode.hedera.com/api/v1/accounts'

async function resolve(evmAddress, label) {
  const res = await fetch(`${MIRROR}/${evmAddress}`)
  if (!res.ok) {
    throw new Error(
      `${label} (${evmAddress}) not found on testnet mirror node yet (HTTP ${res.status}). ` +
        `Fund it at https://portal.hedera.com/faucet first, then retry.`
    )
  }
  const data = await res.json()
  console.log(`${label}: ${evmAddress} -> ${data.account} (balance: ${data.balance?.balance ?? '?'} tinybar)`)
  return data.account
}

const clientId = await resolve(process.env.HEDERA_CLIENT_EVM_ADDRESS, 'Client (payer)')
const serverId = await resolve(process.env.HEDERA_SERVER_EVM_ADDRESS, 'Server (payee)')

const envPath = new URL('./.env', import.meta.url)
let contents = readFileSync(envPath, 'utf8')
contents = contents.replace(/^HEDERA_CLIENT_ACCOUNT_ID=.*$/m, `HEDERA_CLIENT_ACCOUNT_ID=${clientId}`)
contents = contents.replace(/^HEDERA_SERVER_ACCOUNT_ID=.*$/m, `HEDERA_SERVER_ACCOUNT_ID=${serverId}`)
writeFileSync(envPath, contents)
console.log(`\nWrote account IDs into ${envPath.pathname}`)
