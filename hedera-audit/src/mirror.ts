import { parseMessage, verifyOpening } from './commitment.ts'
import type { AuditMessage, AuditOpening } from './types.ts'

export const HASHSCAN_TESTNET_URL = 'https://hashscan.io/testnet'

export type FetchFn = (
  url: string,
  init?: { signal?: AbortSignal }
) => Promise<{
  ok: boolean
  status: number
  json(): Promise<unknown>
}>

interface MirrorTopicMessage {
  consensus_timestamp: string
  message: string
  sequence_number: number
  payer_account_id?: string
}

export interface TopicEntry {
  sequenceNumber: number
  consensusTimestamp: string
  payerAccountId: string | null
  /** Null when the topic message is not a well-formed v1 audit message. */
  message: AuditMessage | null
  hashscanUrl: string
}

export type EntryStatus = 'verified' | 'unopened' | 'malformed'

export interface VerifiedEntry extends TopicEntry {
  status: EntryStatus
  opening: AuditOpening | null
}

export function hashscanTopicUrl(topicId: string, base: string = HASHSCAN_TESTNET_URL): string {
  return `${base}/topic/${topicId}`
}

export function hashscanMessageUrl(consensusTimestamp: string, base: string = HASHSCAN_TESTNET_URL): string {
  return `${base}/transaction/${consensusTimestamp}`
}

export class MirrorTopicReader {
  readonly #baseUrl: string
  readonly #fetch: FetchFn
  readonly #hashscanBase: string

  constructor(params: { mirrorBaseUrl: string; fetch?: FetchFn; hashscanBaseUrl?: string }) {
    this.#baseUrl = params.mirrorBaseUrl.replace(/\/+$/, '')
    this.#fetch = params.fetch ?? ((url, init) => fetch(url, init))
    this.#hashscanBase = params.hashscanBaseUrl ?? HASHSCAN_TESTNET_URL
  }

  /** Reads up to `limit` messages in consensus order, starting after `afterSequence`. */
  async listEntries(topicId: string, opts: { afterSequence?: number; limit?: number } = {}): Promise<TopicEntry[]> {
    const url = new URL(`${this.#baseUrl}/api/v1/topics/${topicId}/messages`)
    url.searchParams.set('order', 'asc')
    const limit = opts.limit ?? 100
    if (!Number.isSafeInteger(limit) || limit < 1) throw new Error('limit must be a positive integer')
    url.searchParams.set('limit', String(limit))
    const after = opts.afterSequence ?? 0
    if (!Number.isSafeInteger(after) || after < 0) throw new Error('afterSequence must be a non-negative integer')
    // The mirror node rejects sequencenumber=gt:0 with HTTP 400, so the first page omits the filter.
    if (after > 0) url.searchParams.set('sequencenumber', `gt:${after}`)
    const res = await this.#fetch(url.toString(), { signal: AbortSignal.timeout(8_000) })
    if (!res.ok) throw new Error(`mirror node returned ${res.status} for topic ${topicId}`)
    const body = (await res.json()) as { messages?: MirrorTopicMessage[] }
    return (body.messages ?? []).map((m) => ({
      sequenceNumber: m.sequence_number,
      consensusTimestamp: m.consensus_timestamp,
      payerAccountId: m.payer_account_id ?? null,
      message: parseMessage(Buffer.from(m.message, 'base64')),
      hashscanUrl: hashscanMessageUrl(m.consensus_timestamp, this.#hashscanBase),
    }))
  }
}

/** Matches each topic entry to the opening that reproduces its commitment. */
export function verifyEntries(entries: TopicEntry[], openings: AuditOpening[]): VerifiedEntry[] {
  return entries.map((entry) => {
    if (!entry.message) return { ...entry, status: 'malformed', opening: null }
    const message = entry.message
    const opening = openings.find((o) => verifyOpening(message, o)) ?? null
    return { ...entry, status: opening ? 'verified' : 'unopened', opening }
  })
}
