import { proxyBrambledGet } from '@/lib/brambled-server'

export async function GET() {
  return proxyBrambledGet('/peers')
}
