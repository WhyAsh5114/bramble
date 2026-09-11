// EAC role-scoping helpers shared by every admincli command and by Gate
// 2.1's test. See docs/adr/0004-admin-cli-writes-directly-to-registry.md for
// the design this encodes: enrollment, rotation, and revocation are three
// distinct, separately-grantable EAC roles.
import { hmac } from '@noble/hashes/hmac'
import { sha256 } from '@noble/hashes/sha2'
import { hkdf } from '@noble/hashes/hkdf'
import { x25519 } from '@noble/curves/ed25519'
import { bytesToHex, hexToBytes, utf8ToBytes } from '@noble/hashes/utils'
import { encodeFunctionData, parseAbi } from 'viem'

// Write surface — deliberately separate from sidecar/src/ens/config.ts's
// read-only registryAbi (that file's own comment: "Read-only surface only —
// no write functions here. This sidecar's Section A scope is resolution,
// not enrollment/revocation"). Mirrors the write-ABI duplication pattern
// already used in scripts/gate0.3-eac-check and scripts/provision-dev-tailnet.
export const registryWriteAbi = parseAbi([
  'function register(string label, address owner, address registry, address resolver, uint256 roleBitmap, uint64 expiry) returns (uint256 tokenId)',
  'function grantRootRoles(uint256 roleBitmap, address account) returns (bool)',
])

export const resolverWriteAbi = parseAbi([
  'function setText(bytes name, string key, string value)',
  'function grantSetterRoles(bytes setter, address account) returns (bool)',
])

// pubkeySetter/revokedSetter encode the *setter identity* grantSetterRoles
// scopes a grant to. Only the function selector + record key matter to EAC
// — the name/value arguments here are placeholders, never actually sent on
// chain as a call. Proven pattern: scripts/gate0.3-eac-check/index.mjs used
// the identical technique to scope a grant to a "status" key.
export function pubkeySetter(): `0x${string}` {
  return encodeFunctionData({ abi: resolverWriteAbi, functionName: 'setText', args: ['0x', 'pubkey', ''] })
}

export function revokedSetter(): `0x${string}` {
  return encodeFunctionData({ abi: resolverWriteAbi, functionName: 'setText', args: ['0x', 'revoked', ''] })
}

// aclSetter scopes a grant to the `acl` text key — a device/agent's own
// symbolic service allowlist (comma-separated service names, no host/port;
// see docs/adr/0005-acl-record-schema.md). Independently grantable from
// pubkey/revoked, same as those two are from each other.
export function aclSetter(): `0x${string}` {
  return encodeFunctionData({ abi: resolverWriteAbi, functionName: 'setText', args: ['0x', 'acl', ''] })
}

// aclGrantersSetter scopes a grant to the `acl-granters` text key — a
// *gateway's own* list of identities (ENS labels) it accepts ACL vouches
// from, independently owned by whoever deployed that gateway's resolver.
// See docs/adr/0005-acl-record-schema.md: this is the other half of the
// trust-store model, alongside `acl` above — `acl` controls who may write
// anything about a device at all, `acl-granters` controls whose digests a
// gateway actually honors. Same grantSetterRoles pattern as pubkey/revoked/
// acl, applied a fifth time.
export function aclGrantersSetter(): `0x${string}` {
  return encodeFunctionData({ abi: resolverWriteAbi, functionName: 'setText', args: ['0x', 'acl-granters', ''] })
}

// aclDigestECDH is what actually goes on chain for a service name — never
// the plaintext name itself, and never a value derived from a single shared
// secret either (see docs/adr/0005-acl-record-schema.md for why a bare hash
// and a manually-shared pepper were both rejected). Instead it's a
// Diffie-Hellman shared secret between two already-enrolled identities'
// existing WireGuard (X25519) keys — the granter vouching for the grant, and
// the gateway that will verify it — exactly like two PKI peers agreeing on
// a shared key from nothing but each other's already-public keys. No secret
// is ever transmitted or manually copied: the gateway independently
// recomputes the identical shared secret from its own private key plus the
// granter's already-published pubkey.
//
// Canonical algorithm (must match byte-for-byte with brambled/gateway/digest.go's
// ACLDigestECDH — golang.org/x/crypto/curve25519 for the ECDH, stdlib
// crypto/hmac + a manual HKDF for the rest, already imported in
// brambled/wgnode):
//   sharedSecret = X25519(myPrivateKey32Bytes, theirPublicKey32Bytes)
//   aclKey       = HKDF-SHA256(ikm=sharedSecret, salt=none, info="bramble-acl-v1", length=32)
//   message      = utf8(trim(lowercase(requesterPubkeyHex)) + "|" + trim(lowercase(serviceName)))
//   digest       = hex(HMAC-SHA256(key=aclKey, message=message))
//
// HKDF domain-separates this from WireGuard's own Noise_IK handshake, which
// also consumes these same keys for an unrelated protocol — reusing a raw
// ECDH output directly across two different protocols is the thing HKDF
// exists to avoid, not a hypothetical concern.
//
// requesterPubkeyHex binds the digest to the specific device's own pubkey it
// will be written under — without this, since acl records are public
// (adr/0005's Context section), any device able to write its own acl record
// could copy a digest read off another device's public record and gain the
// same grant. It's mixed into the HMAC message the same way serviceName is,
// not used as X25519 key material.
const ACL_HKDF_INFO = utf8ToBytes('bramble-acl-v1')

export function aclDigestECDH(
  myPrivateKeyHex: string,
  theirPublicKeyHex: string,
  requesterPubkeyHex: string,
  serviceName: string
): string {
  const sharedSecret = x25519.getSharedSecret(hexToBytes(myPrivateKeyHex), hexToBytes(theirPublicKeyHex))
  const aclKey = hkdf(sha256, sharedSecret, undefined, ACL_HKDF_INFO, 32)
  const canonicalRequester = requesterPubkeyHex.trim().toLowerCase()
  const canonicalService = serviceName.trim().toLowerCase()
  const message = `${canonicalRequester}|${canonicalService}`
  return bytesToHex(hmac(sha256, aclKey, utf8ToBytes(message)))
}
