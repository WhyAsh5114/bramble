// Revokes (or un-revokes) a device via the EAC-gated `revoked` text record
// (docs/adr/0004) — distinct from scripts/provision-dev-tailnet/set-pubkey.ts's
// clear-pubkey revocation path, which stays in place unchanged for Gate
// 1.3's already-verified runbook. This is the role-scoped path: an account
// holding ROLE_SET_TEXT scoped to "revoked" (not "pubkey") can revoke
// without ever being able to rotate a device's key.
//
// Usage: bun run revoke.ts <device-label> <true|false>
import { setDeviceRevoked } from './lib'
import { cliDeps } from './lib-viem'

async function main() {
  const [deviceLabel, value] = process.argv.slice(2)
  if (!deviceLabel || (value !== 'true' && value !== 'false')) {
    throw new Error('usage: bun run revoke.ts <device-label> <true|false>')
  }
  await setDeviceRevoked(cliDeps(), { deviceLabel, value: value === 'true' })
}

main().catch((err) => {
  console.error('\nrevoke failed:', err)
  process.exit(1)
})
