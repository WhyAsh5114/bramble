// One-time provisioning step for Gate 3.2: encrypts the granter's
// ACL-capability private key under wallet-cli's Key Ring so it never has
// to live as plaintext in .env again. Run once, on the same machine that
// already did `wallet-cli ring init` with the device present (see
// docs/adr/0001-ledger-ring-vs-send-split.md's "Pivot" section — decrypt
// is headless afterward, on that same machine only).
//
// Usage: BRAMBLE_GRANTER_PRIVATE_KEY=<hex> bun run provision-granter-key
// (bare hex, no 0x — same convention as set-acl.ts). Reads from the env
// var rather than a CLI arg so the key never appears in shell history or
// `ps`. After this succeeds, delete BRAMBLE_GRANTER_PRIVATE_KEY from .env
// — set-acl.ts reads the ring-encrypted file exclusively, never that env
// var, so leaving stale plaintext there afterward is pure risk.
import { existsSync, writeFileSync } from 'node:fs'
import { ringEncrypt } from './ledger-ring'
import { GRANTER_KEY_RING_PATH } from './granter-key-path'

async function main() {
  const plaintext = process.env.BRAMBLE_GRANTER_PRIVATE_KEY?.trim()
  if (!plaintext) {
    throw new Error("BRAMBLE_GRANTER_PRIVATE_KEY not set — export the granter's hex private key for this one run only")
  }
  if (existsSync(GRANTER_KEY_RING_PATH)) {
    throw new Error(
      `${GRANTER_KEY_RING_PATH} already exists — refusing to overwrite. ` +
        `Delete it first if you're deliberately re-provisioning.`
    )
  }

  const ciphertext = await ringEncrypt(plaintext)
  writeFileSync(GRANTER_KEY_RING_PATH, ciphertext, { mode: 0o600 })
  console.log(`encrypted granter key written to ${GRANTER_KEY_RING_PATH}`)
  console.log('now delete BRAMBLE_GRANTER_PRIVATE_KEY from .env — set-acl.ts no longer reads it.')
}

main().catch((err) => {
  console.error('\nprovision-granter-key failed:', err)
  process.exit(1)
})
