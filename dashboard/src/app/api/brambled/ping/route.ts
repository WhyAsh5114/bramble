import { NextRequest } from 'next/server'
import { pingPeer, trackedPeers } from '@/lib/brambled-server'

// The one non-GET route in the dashboard's server. It still can't be turned
// into an arbitrary prober: label is checked against brambled's own current
// peer table (re-fetched here, not trusted from any client-supplied data
// beyond the label string itself) before anything is forwarded, and
// brambled's own /ping handler independently re-derives the target address
// from that label — this route never accepts or forwards a host/IP.
export async function POST(req: NextRequest) {
  let label: unknown
  try {
    ;({ label } = await req.json())
  } catch {
    return Response.json({ error: 'invalid JSON body' }, { status: 400 })
  }
  if (typeof label !== 'string' || !label) {
    return Response.json({ error: 'label (string) is required' }, { status: 400 })
  }

  try {
    const peers = await trackedPeers()
    if (!peers.some((p) => p.label === label)) {
      return Response.json({ error: 'label is not a peer brambled currently tracks' }, { status: 400 })
    }
    return Response.json(await pingPeer(label))
  } catch (err) {
    return Response.json({ error: err instanceof Error ? err.message : String(err) }, { status: 502 })
  }
}
