// Wraps `wallet-cli ring encrypt`/`ring decrypt` for the granter's
// ACL-capability private key (Gate 3.2, docs/adr/0001-ledger-ring-vs-send-
// split.md's "Pivot" section). This key is never used to sign anything —
// it's an X25519 scalar fed into aclDigestECDH (roles.ts) — so a signing
// device can't protect it; only a hardware-rooted encryption primitive
// (Key Ring) can. Text always crosses the wallet-cli boundary via
// stdin/stdout, never a plaintext intermediate file: decrypt() only ever
// returns the key in memory, for the caller's own brief use at grant time.
//
// Same-host only, by design (adr/0001's retarget rationale): the admin's
// workstation runs `wallet-cli ring init` once, with the device present;
// every decrypt after that is headless on that same machine (verified
// Sept 10 with the device physically unplugged) — never shipped to, or
// usable from, any other host.
import { spawn } from 'node:child_process'

// Namespaces the derived encryption key inside the ring — not a secret
// itself, just a label. Must match between encrypt() (provision-granter-
// key.ts, run once) and every decrypt() call here.
const RING_KEY_NAME = 'bramble-granter-key'

function runRing(subcommand: 'encrypt' | 'decrypt', input: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const proc = spawn('wallet-cli', ['ring', subcommand, '-k', RING_KEY_NAME], {
      stdio: ['pipe', 'pipe', 'inherit'],
    })
    let stdout = ''
    proc.stdout.on('data', (chunk: Buffer) => {
      stdout += chunk.toString('utf8')
    })
    proc.on('error', (err) => reject(new Error(`failed to spawn wallet-cli: ${err.message}`)))
    proc.on('close', (code) => {
      if (code !== 0) {
        reject(new Error(`wallet-cli ring ${subcommand} exited with code ${code}`))
        return
      }
      resolve(stdout.trim())
    })
    proc.stdin.write(input)
    proc.stdin.end()
  })
}

// encrypt is only ever called once, interactively, by provision-granter-
// key.ts — never by set-acl.ts itself, which must only ever decrypt.
export function ringEncrypt(plaintext: string): Promise<string> {
  return runRing('encrypt', plaintext)
}

export function ringDecrypt(ciphertext: string): Promise<string> {
  return runRing('decrypt', ciphertext)
}
