import { buildAudit, encodeMessage, type RandomBytes } from './commitment.ts'
import type { AuditEvent, AuditRecord, TopicClient } from './types.ts'

export const TOPIC_MEMO = 'bramble audit trail v1'
const TOPIC_ID = /^\d+\.\d+\.\d+$/

export async function createAuditTopic(client: TopicClient): Promise<string> {
  const { topicId } = await client.createTopic({ memo: TOPIC_MEMO })
  if (!TOPIC_ID.test(topicId)) throw new Error('Hedera client returned a malformed topic ID')
  return topicId
}

export class HcsAuditTrail {
  readonly #client: TopicClient
  readonly #topicId: string
  readonly #randomBytes: RandomBytes | undefined

  constructor(params: { client: TopicClient; topicId: string; randomBytes?: RandomBytes }) {
    if (!TOPIC_ID.test(params.topicId)) throw new Error('topicId must look like 0.0.1234')
    this.#client = params.client
    this.#topicId = params.topicId
    this.#randomBytes = params.randomBytes
  }

  get topicId(): string {
    return this.#topicId
  }

  /**
   * Posts one salted commitment. The returned opening holds the plaintext and the salt:
   * the caller keeps it and shows it to a verifier. It is never posted.
   */
  async record(event: AuditEvent): Promise<AuditRecord> {
    const { message, opening } = buildAudit(event, this.#randomBytes)
    const { transactionId, sequenceNumber } = await this.#client.submitMessage({
      topicId: this.#topicId,
      message: encodeMessage(message),
    })
    return { message, opening, receipt: { topicId: this.#topicId, transactionId, sequenceNumber } }
  }
}
