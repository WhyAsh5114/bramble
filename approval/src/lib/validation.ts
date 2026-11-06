import { ApiError, type RequestInput } from './contracts'

export function label(value: unknown, name: string): string {
  if (typeof value !== 'string' || !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(value)) {
    throw new ApiError(400, 'invalid_request', `${name} must be a lowercase ENS label of 1–63 characters.`)
  }
  return value
}

export async function readInput(request: Request): Promise<RequestInput> {
  if (request.headers.get('content-type')?.split(';')[0].trim() !== 'application/json') {
    throw new ApiError(415, 'unsupported_media_type', 'Use application/json.')
  }
  const reader = request.body?.getReader()
  const chunks: Uint8Array[] = []
  let size = 0
  if (reader) {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      size += value.byteLength
      if (size > 16_384) {
        await reader.cancel()
        throw new ApiError(413, 'request_too_large', 'Request body exceeds 16384 bytes.')
      }
      chunks.push(value)
    }
  }
  let body: unknown
  try {
    body = JSON.parse(Buffer.concat(chunks).toString('utf8'))
  } catch {
    throw new ApiError(400, 'invalid_json', 'Request body must contain valid JSON.')
  }
  if (!body || typeof body !== 'object' || Array.isArray(body)) {
    throw new ApiError(400, 'invalid_request', 'Request body must be an object.')
  }
  const value = body as Record<string, unknown>
  if (
    Object.keys(value).some(
      (key) => !['agentLabel', 'gatewayLabel', 'service', 'note', 'requestedDuration'].includes(key)
    )
  ) {
    throw new ApiError(400, 'invalid_request', 'Request body contains unknown fields.')
  }
  const agentLabel = label(value.agentLabel, 'agentLabel')
  const gatewayLabel = label(value.gatewayLabel, 'gatewayLabel')
  if (typeof value.service !== 'string' || !/^[a-z][a-z0-9_-]{0,63}$/.test(value.service)) {
    throw new ApiError(400, 'invalid_request', 'service must be a lowercase symbolic name of 1–64 characters.')
  }
  if (value.note !== undefined && (typeof value.note !== 'string' || value.note.length > 2000)) {
    throw new ApiError(400, 'invalid_request', 'note must be a string of at most 2000 characters.')
  }
  if (
    value.requestedDuration !== undefined &&
    (typeof value.requestedDuration !== 'number' ||
      !Number.isInteger(value.requestedDuration) ||
      value.requestedDuration < 1 ||
      value.requestedDuration > 86400)
  ) {
    throw new ApiError(400, 'invalid_request', 'requestedDuration must be integer seconds from 1 to 86400.')
  }
  return {
    agentLabel,
    gatewayLabel,
    service: value.service,
    ...(value.note === undefined ? {} : { note: value.note as string }),
    ...(value.requestedDuration === undefined ? {} : { requestedDuration: value.requestedDuration as number }),
  }
}
