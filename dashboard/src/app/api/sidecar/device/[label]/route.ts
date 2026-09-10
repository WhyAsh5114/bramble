import { proxySidecarGet } from '@/lib/sidecar-server'

export async function GET(_request: Request, { params }: { params: Promise<{ label: string }> }) {
  const { label } = await params
  return proxySidecarGet(`/device/${encodeURIComponent(label)}`)
}
