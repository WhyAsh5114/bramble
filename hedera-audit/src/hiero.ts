import { Client, PrivateKey, TopicCreateTransaction, TopicMessageSubmitTransaction } from '@hiero-ledger/sdk'
import type { TopicClient } from './types.ts'

/** Mainnet is deliberately not an option: this module never writes to a mainnet. */
export type AuditNetwork = 'testnet' | 'previewnet'

export interface HieroConfig {
  accountId: string
  /** ECDSA private key string, the same format the x402 payer uses. */
  privateKey: string
  network?: AuditNetwork
}

/** The real Hedera client. Topics it creates only accept messages signed by the payer key. */
export function createHieroTopicClient(config: HieroConfig): TopicClient {
  const network = config.network ?? 'testnet'
  if (network !== 'testnet' && network !== 'previewnet')
    throw new Error('audit trail supports testnet and previewnet only')
  const client = network === 'testnet' ? Client.forTestnet() : Client.forPreviewnet()
  const key = PrivateKey.fromStringECDSA(config.privateKey)
  client.setOperator(config.accountId, key)

  return {
    async createTopic({ memo }) {
      const response = await new TopicCreateTransaction().setTopicMemo(memo).setSubmitKey(key.publicKey).execute(client)
      const receipt = await response.getReceipt(client)
      if (!receipt.topicId) throw new Error('topic creation receipt has no topic ID')
      return { topicId: receipt.topicId.toString() }
    },
    async submitMessage({ topicId, message }) {
      const response = await new TopicMessageSubmitTransaction().setTopicId(topicId).setMessage(message).execute(client)
      const receipt = await response.getReceipt(client)
      const transactionId = response.transactionId.toString()
      if (!receipt.topicSequenceNumber)
        throw new Error(`submit receipt for ${transactionId} has no topic sequence number`)
      return { transactionId, sequenceNumber: receipt.topicSequenceNumber.toString() }
    },
  }
}
