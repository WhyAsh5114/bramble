import type { FetchFn } from '../src/mirror.ts'
import type { TopicClient } from '../src/types.ts'

export class FakeTopicClient implements TopicClient {
  readonly topics: { topicId: string; memo: string }[] = []
  readonly submitted: { topicId: string; message: Uint8Array }[] = []
  failNextSubmit = false

  async createTopic({ memo }: { memo: string }) {
    const topicId = `0.0.${9000 + this.topics.length}`
    this.topics.push({ topicId, memo })
    return { topicId }
  }

  async submitMessage({ topicId, message }: { topicId: string; message: Uint8Array }) {
    if (this.failNextSubmit) {
      this.failNextSubmit = false
      throw new Error('simulated network failure')
    }
    this.submitted.push({ topicId, message })
    return {
      transactionId: `0.0.1001@1700000000.${this.submitted.length}`,
      sequenceNumber: String(this.submitted.length),
    }
  }

  /** What the mirror node would serve for `topicId`, in its REST shape. */
  mirrorBody(topicId: string) {
    return {
      messages: this.submitted
        .map((s, i) => ({ s, sequence_number: i + 1 }))
        .filter(({ s }) => s.topicId === topicId)
        .map(({ s, sequence_number }) => ({
          consensus_timestamp: `1700000000.00000000${sequence_number}`,
          message: Buffer.from(s.message).toString('base64'),
          sequence_number,
          payer_account_id: '0.0.1001',
        })),
    }
  }
}

export function fakeFetch(body: unknown, status = 200): FetchFn & { urls: string[] } {
  const urls: string[] = []
  const fn = (async (url: string) => {
    urls.push(url)
    return { ok: status >= 200 && status < 300, status, json: async () => body }
  }) as FetchFn & { urls: string[] }
  fn.urls = urls
  return fn
}
