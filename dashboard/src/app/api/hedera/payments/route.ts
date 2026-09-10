import { HEDERA_MIRROR_NODE_URL, HEDERA_USDC_ASSET_ID, hederaWatchAccountIds } from '@/lib/config'
import type { HederaPayment } from '@/lib/types'

// Hedera's own public testnet mirror node -- no API key, no auth, the same
// source every gate in this project's build plan used to independently
// confirm a settlement actually happened (rather than trusting the client's
// own "it worked" log line). Read-only, no secrets: this route only ever
// asks "what happened to this public account," which is exactly what
// HashScan itself shows.
interface MirrorTokenTransfer {
  token_id: string
  account: string
  amount: number
}

interface MirrorTransaction {
  transaction_id: string
  consensus_timestamp: string
  result: string
  token_transfers: MirrorTokenTransfer[]
}

interface MirrorTransactionsResponse {
  transactions: MirrorTransaction[]
}

async function fetchAccountTransactions(accountId: string): Promise<MirrorTransaction[]> {
  const url = new URL('/api/v1/transactions', HEDERA_MIRROR_NODE_URL)
  url.searchParams.set('account.id', accountId)
  url.searchParams.set('order', 'desc')
  url.searchParams.set('limit', '25')
  const res = await fetch(url, { cache: 'no-store', signal: AbortSignal.timeout(8_000) })
  if (!res.ok) throw new Error(`mirror node returned ${res.status} for ${accountId}`)
  const body = (await res.json()) as MirrorTransactionsResponse
  return body.transactions
}

// transaction_id looks like "0.0.7162784-1789020116-357753981" -- HashScan
// wants the consensus form, "0.0.7162784@1789020116.357753981".
function hashscanTransactionId(rawId: string): string {
  const [account, seconds, nanos] = rawId.split('-')
  return `${account}@${seconds}.${nanos}`
}

export async function GET() {
  const accountIds = hederaWatchAccountIds()
  if (accountIds.length === 0) {
    return Response.json({ error: 'HEDERA_WATCH_ACCOUNT_IDS not configured' }, { status: 501 })
  }

  try {
    const perAccount = await Promise.all(accountIds.map(fetchAccountTransactions))
    const byId = new Map<string, MirrorTransaction>()
    for (const tx of perAccount.flat()) byId.set(tx.transaction_id, tx)

    const payments: HederaPayment[] = [...byId.values()]
      .filter((tx) => tx.result === 'SUCCESS')
      .flatMap((tx) => {
        const usdc = tx.token_transfers.filter((t) => t.token_id === HEDERA_USDC_ASSET_ID)
        const credit = usdc.find((t) => t.amount > 0)
        const debit = usdc.find((t) => t.amount < 0)
        if (!credit || !debit) return []
        return [
          {
            transactionId: tx.transaction_id,
            consensusTimestamp: tx.consensus_timestamp,
            amount: (credit.amount / 1_000_000).toFixed(6).replace(/\.?0+$/, ''),
            from: debit.account,
            to: credit.account,
            hashscanUrl: `https://hashscan.io/testnet/transaction/${hashscanTransactionId(tx.transaction_id)}`,
          },
        ]
      })
      .sort((a, b) => Number(b.consensusTimestamp) - Number(a.consensusTimestamp))

    return Response.json(payments)
  } catch (err) {
    return Response.json({ error: err instanceof Error ? err.message : String(err) }, { status: 502 })
  }
}
