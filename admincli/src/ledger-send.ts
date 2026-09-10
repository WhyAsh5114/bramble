// Wraps `wallet-cli send --data <calldata>` — the on-device-confirmed
// signing path Gate 3.1 (docs/05_BUILD_PLAN.md) requires for enroll,
// revoke, and ACL-widen. No software signing path exists alongside this
// for those three scripts: the viem wallet client they used before is
// deleted, not left as a fallback, for the same reason Gate 3.2 deleted
// the plaintext-env granter-key path — a surviving software-sign option
// would itself be the bypass the gate prohibits, whether or not it's used.
import { spawn } from 'node:child_process'
import type { Address, Hash, Hex } from 'viem'

// Generous on purpose — this is a human finding and pressing a physical
// button, not a network call. wallet-cli's own --device-timeout default
// (60000ms) assumes the device is already unlocked and in hand.
const DEVICE_TIMEOUT_MS = 120_000

function ledgerAccountLabel(): string {
  const label = process.env.BRAMBLE_LEDGER_ACCOUNT?.trim()
  if (!label) {
    throw new Error(
      'BRAMBLE_LEDGER_ACCOUNT not set — run `wallet-cli account discover` once with the device ' +
        'connected, then set this to the session label it prints (e.g. ethereum-sepolia-1)'
    )
  }
  return label
}

// wallet-cli's --output json shape for a successful `send` hasn't been
// observed against a real device yet (Gate 3.1's physical run is still
// pending — see docs/05_BUILD_PLAN.md). Try the field names a Ledger CLI
// would plausibly use first, then fall back to scanning raw stdout for
// anything hash-shaped, so a field-name guess that turns out wrong on the
// first real run doesn't hard-fail the whole path.
function extractTxHash(output: string): Hash {
  try {
    const parsed = JSON.parse(output)
    const candidate =
      parsed?.data?.hash ?? parsed?.data?.txHash ?? parsed?.data?.transactionHash ?? parsed?.hash ?? parsed?.txHash
    if (typeof candidate === 'string' && /^0x[0-9a-fA-F]{64}$/.test(candidate)) return candidate as Hash
  } catch {
    // not JSON, or not the shape guessed above — fall through to the scan
  }
  const match = output.match(/0x[0-9a-fA-F]{64}/)
  if (!match) throw new Error(`could not find a transaction hash in wallet-cli send output:\n${output}`)
  return match[0] as Hash
}

// sendViaDevice signs and broadcasts one EVM contract call through the
// Ledger, requiring physical confirmation. `to`/`data` are exactly what
// viem's writeContract would have sent to the node — callers build them
// with encodeFunctionData instead of calling writeContract directly. A
// disconnected/locked/rejecting device makes the subprocess exit non-zero,
// which this rejects — the "no software path bypasses it" assertion is
// structural, not a separate check.
export function sendViaDevice(to: Address, data: Hex): Promise<Hash> {
  const account = ledgerAccountLabel()
  return new Promise((resolve, reject) => {
    const proc = spawn(
      'wallet-cli',
      [
        'send',
        '--account',
        account,
        '--to',
        to,
        '--amount',
        '0 ETH',
        '--data',
        data,
        '--device-timeout',
        String(DEVICE_TIMEOUT_MS),
        '--output',
        'json',
      ],
      { stdio: ['ignore', 'pipe', 'inherit'] }
    )
    let stdout = ''
    proc.stdout.on('data', (chunk: Buffer) => {
      stdout += chunk.toString('utf8')
    })
    proc.on('error', (err) => reject(new Error(`failed to spawn wallet-cli: ${err.message}`)))
    proc.on('close', (code) => {
      if (code !== 0) {
        reject(new Error(`wallet-cli send exited with code ${code} — device disconnected, rejected, or timed out`))
        return
      }
      try {
        resolve(extractTxHash(stdout))
      } catch (err) {
        reject(err)
      }
    })
  })
}
