import { createPaymentsApi } from '../../../lib/payments'

export const runtime = 'nodejs'
export const dynamic = 'force-dynamic'
export const GET = createPaymentsApi({
  config: () => process.env.APPROVAL_PAYMENT_AGENTS,
  perPaymentCap: () => process.env.HEDERA_MAX_PAYMENT_ATOMIC,
})
