import { proxySidecarGet } from '@/lib/sidecar-server'

export async function GET() {
  return proxySidecarGet('/relays')
}
