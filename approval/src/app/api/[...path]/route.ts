import { createApprovalApi } from '../../../lib/api'

export const runtime = 'nodejs'
export const dynamic = 'force-dynamic'

// One long-lived Node process owns the queue and timers. ETH-30 wires real adapters here.
const api = createApprovalApi({ token: () => process.env.APPROVAL_DEMO_TOKEN })

export const GET = (request: Request) => api.handle(request)
export const POST = (request: Request) => api.handle(request)
