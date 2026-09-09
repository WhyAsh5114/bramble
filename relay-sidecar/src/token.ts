// Mints the self-contained, HMAC-signed rendezvous token described in
// docs/adr/0007. The Go relay (relay/token.go) verifies these entirely
// locally — no callback to this sidecar on the per-connection hot path —
// so the wire format here must match that file byte-for-byte:
//
//   payload = {"exp":<unix seconds>,"nonce":"<32 hex chars>"}   (fixed key order)
//   token   = base64url(payload) + "." + hex(HMAC-SHA256(secret, base64url(payload)))
import { createHmac, randomBytes } from 'node:crypto'
import { TOKEN_TTL_SECONDS } from './config'

export type MintedToken = {
  token: string
  expiresAt: number
}

export function mintToken(secretHex: string): MintedToken {
  const exp = Math.floor(Date.now() / 1000) + TOKEN_TTL_SECONDS
  const nonce = randomBytes(16).toString('hex')
  // Fixed key order — exp then nonce — so the Go side's own JSON encoding
  // (which also writes them in this order) produces an identical payload.
  const payload = `{"exp":${exp},"nonce":"${nonce}"}`
  const payloadB64 = Buffer.from(payload, 'utf8').toString('base64url')
  const sig = createHmac('sha256', Buffer.from(secretHex, 'hex')).update(payloadB64).digest('hex')
  return { token: `${payloadB64}.${sig}`, expiresAt: exp }
}
