export const AUDIT_SECRET_NAMES = ['HEDERA_CLIENT_ACCOUNT_ID', 'HEDERA_CLIENT_PRIVATE_KEY'] as const

/** Returns the names (never the values) of audit secrets missing from `env`. */
export function missingAuditSecrets(env: Record<string, string | undefined>): string[] {
  return AUDIT_SECRET_NAMES.filter((name) => !env[name])
}
