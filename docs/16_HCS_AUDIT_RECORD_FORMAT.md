# HCS audit trail: record format

**Kind:** Reference. **Workstream:** W10, option A (`docs/15_MUMBAI_HANDOFF.md` section 5.2). **Code:** `hedera-audit/`. **Status of the code:** unit-tested against a fake Hedera client. Not yet run against Hedera testnet (Open: needs the payer secrets, see "Live run").

Bramble posts one message to a Hedera Consensus Service (HCS) topic for each grant, revoke and payment. The message holds a salted commitment, never the event itself.

## Message on the topic

The message is UTF-8 JSON with exactly three keys. It is at most 95 bytes, well under the HCS single-message limit of 1024 bytes.

| Key    | Type    | Meaning                                                                    |
| ------ | ------- | -------------------------------------------------------------------------- |
| `v`    | integer | Format version. Always `1`.                                                |
| `kind` | string  | `grant`, `revoke` or `payment`.                                            |
| `c`    | string  | Commitment: 64 lowercase hex characters (HMAC-SHA256, see below).          |

Example:

```json
{ "v": 1, "kind": "grant", "c": "3f0c…64 hex characters…a9" }
```

A reader treats any message that does not match this shape exactly (extra keys, uppercase hex, another version) as malformed and skips it.

## Events

All field values are strings. Every field of an event is required. An event with a missing, empty or extra field is rejected before anything is posted.

| Kind      | Field           | Meaning                                                              |
| --------- | --------------- | -------------------------------------------------------------------- |
| `grant`   | `subject`       | ENS name of the agent or device that receives the grant              |
|           | `serviceDigest` | `0x`-prefixed ACL digest of the service (ADR 0005), never a name     |
|           | `expiresAt`     | Expiry, decimal Unix seconds                                         |
|           | `txRef`         | Hash of the Sepolia transaction that wrote the grant                 |
| `revoke`  | `subject`       | ENS name whose grant ends                                            |
|           | `serviceDigest` | ACL digest of the service                                            |
|           | `txRef`         | Hash of the Sepolia transaction that removed the grant               |
| `payment` | `payer`         | Hedera account ID of the payer                                       |
|           | `payee`         | Hedera account ID of the payee                                       |
|           | `asset`         | Hedera token ID                                                      |
|           | `amountAtomic`  | Amount in atomic units, decimal                                      |
|           | `txRef`         | Hedera transaction ID of the payment                                 |

## Commitment

```
salt       = 32 random bytes
preimage   = lp("bramble-audit-v1") || for each field name n in sorted order, including "kind": lp(n) || lp(value)
c          = HMAC-SHA256(key = salt, message = preimage), lowercase hex
lp(s)      = 4-byte big-endian length of UTF-8(s) || UTF-8(s)
```

Length prefixes make the encoding unambiguous: moving bytes from one field into the next changes the preimage. Field names are sorted by byte order. The `kind` field is part of the preimage, so a commitment for one kind never opens as another.

## Opening

The operator that posts a commitment keeps the **opening**: the event and the salt. The opening is never posted.

```json
{ "v": 1, "event": { "kind": "revoke", "subject": "…", "serviceDigest": "0x…", "txRef": "0x…" }, "salt": "64 hex characters" }
```

To verify, a reader recomputes `c` from the opening and compares it with the posted `c` in constant time. A matching opening proves that the posted entry commits to exactly that event and salt. It does not prove that the asserted grant or payment really happened: the operator chose the event, so check the referenced `txRef` on its own chain. Without the opening, the encoded fields (subject, service digest, amounts, references) stay computationally hidden, because the salt is a fresh secret random value. The metadata in the next section stays public.

## What the topic reveals

| Public                                          | Hidden from anyone without the opening |
| ----------------------------------------------- | -------------------------------------- |
| That an event of a kind occurred, and when      | Who or what the event concerns         |
| How many events of each kind, and their cadence | Service digests, ENS names, amounts    |
| Which account paid to post the message          | Transaction references                 |

- A fresh random salt per record means two equal events produce unrelated commitments. A dictionary attack over plausible ENS names or service digests does not work against a secret 256-bit salt. This hiding holds only while the salt stays secret.
- Event timing and counts are public. Beat 5 (revoke) therefore shows up on the topic as a `revoke` entry at a known time.
- The commitments themselves cannot be matched to each other or to the on-chain ACL digests of ADR 0005 without the salts. Metadata still allows correlation: kind, topic, payer account, timing and cadence are public, so a `revoke` entry that lands just after a known Sepolia revoke transaction is easy to associate with it.
- Event fields are encoded as UTF-8 after rejecting ill-formed UTF-16 (lone surrogates), and the opening stores a frozen snapshot of the validated fields, so one commitment has one event.

## What the audit trail does not do

- **It does not enforce anything.** Peers never read the topic to admit or refuse traffic (handoff invariant 1). The topic is a record, not an access check.
- **It does not prove completeness.** The holder of the submit key can omit events. A reader can show that an entry exists and matches an opening, not that no other event happened.
- **It depends on the operator keeping openings.** If an opening is lost, its entry stays on the topic and can no longer be verified.
- **Ordering comes from Hedera consensus.** The `sequence_number` and `consensus_timestamp` that the mirror node returns are authoritative. The message carries no timestamp of its own.

## Topic

- Memo: `bramble audit trail v1`.
- The topic has a **submit key**, set to the public key of the payer account, so only the payer can post. It has **no admin key**, so nobody can delete it or update its keys or memo. Its expiration can still be extended (Hedera allows that without an admin key).
- Network: testnet or previewnet. The code refuses any other value; it never writes to mainnet.

## Reading the topic

The reader in this module (`MirrorTopicReader`) reads entries from the Hedera mirror node REST API. The console is planned to use it; no console integration exists yet.

The first page omits the `sequencenumber` filter, because the mirror node rejects `gt:0` with HTTP 400:

```
GET {mirror}/api/v1/topics/{topicId}/messages?order=asc&limit=100&sequencenumber=gt:{n}
```

The `message` field of each result is base64. `hashscanMessageUrl` and `hashscanTopicUrl` build `https://hashscan.io/testnet/transaction/{consensus_timestamp}` and `https://hashscan.io/testnet/topic/{topicId}`. Verified (reviewer, public read-only testnet requests): the messages route, `order`, `limit`, `sequencenumber=gt:{n}` for n > 0, and base64 decoding. Read from source: the HashScan URL shapes. Open: browser check of entries this module submits.

## Live run

`pnpm --filter @bramble/hedera-audit live-check` creates a topic, posts one commitment per kind, reads them back and verifies each opening. It needs `HEDERA_CLIENT_ACCOUNT_ID` and `HEDERA_CLIENT_PRIVATE_KEY` (an ECDSA key, the same payer the x402 client uses). Without them it prints the missing names and exits with code 2. Verified: the exit-2 path without secrets. Open: the live path.
