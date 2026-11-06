// The demo's machines, in start order, and the facts the launcher and the
// preflight share. Unit names, ports and env names come from deploy/REFERENCE.md
// (ETH-49) and the sidecar and brambled sources. Nothing here is a secret.

export const ROLES = ['relays', 'gateway', 'worker', 'admin'] as const
export type Role = (typeof ROLES)[number]

/** Roles whose machine runs a `brambled serve` node and its sidecar. */
export const NODE_ROLES = ['gateway', 'worker'] as const
export type NodeRole = (typeof NODE_ROLES)[number]

export const SIDECAR_PORT = 7890
export const STATUS_API_PORT = 7899

export const ENV_DIR = '/etc/bramble'

export function isRole(value: string): value is Role {
  return (ROLES as readonly string[]).includes(value)
}

export function isNodeRole(role: Role): role is NodeRole {
  return (NODE_ROLES as readonly string[]).includes(role)
}

/** Parses a comma-separated role list. Throws on an unknown role. */
export function parseRoles(arg: string): Role[] {
  const roles: Role[] = []
  for (const part of arg.split(',')) {
    const name = part.trim()
    if (name === '') continue
    if (!isRole(name)) throw new Error(`unknown role "${name}" (expected ${ROLES.join(', ')})`)
    if (!roles.includes(name)) roles.push(name)
  }
  return ROLES.filter((r) => roles.includes(r))
}

/** Orders roles in start order and drops duplicates. */
export function sortRoles(roles: Iterable<Role>): Role[] {
  const set = new Set(roles)
  return ROLES.filter((r) => set.has(r))
}

const INSTANCE = /^[A-Za-z0-9_-]+$/

/** Splits a comma-separated relay instance list. Throws on a name unsafe for a unit name. */
export function parseRelayInstances(arg: string | undefined): string[] {
  const names = (arg ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '')
  for (const n of names) if (!INSTANCE.test(n)) throw new Error(`invalid relay instance name "${n}"`)
  return names.length > 0 ? names : ['relay1']
}

export interface UnitOptions {
  relayInstances: string[]
  /** When set, nodes run in a NAT box and its unit must start first. */
  natBox: boolean
}

/**
 * Units the relays machine also needs: the logging RPC proxy (`rpc-proxy.service`,
 * ETH-44), Caddy and coturn (distribution packages). `pnpm demo` does not start
 * them, because the installer does not create or enable them yet. It prints the
 * command, and the preflight checks they are active.
 */
export const RELAY_SUPPORT_UNITS = ['rpc-proxy', 'caddy', 'coturn']

/**
 * The systemd units a role needs, in start order. Source: deploy/REFERENCE.md.
 * The relays machine's support units are in RELAY_SUPPORT_UNITS.
 */
export function unitsFor(role: Role, opts: UnitOptions): string[] {
  const nat = opts.natBox ? ['bramble-nat-box'] : []
  switch (role) {
    case 'relays':
      return opts.relayInstances.map((i) => `bramble-relay@${i}`)
    case 'gateway':
      return [...nat, 'bramble-demo-service', 'bramble-gateway']
    case 'worker':
      return [...nat, 'bramble-worker']
    case 'admin':
      return ['bramble-approval', 'bramble-console-tunnel']
  }
}

/** Env file names the installer seeds under ENV_DIR, one per role (deploy/REFERENCE.md). */
export function rolesFromEnvFileNames(names: string[]): { roles: Role[]; relayInstances: string[] } {
  const roles = new Set<Role>()
  const relayInstances: string[] = []
  for (const name of names) {
    if (name === 'gateway.env') roles.add('gateway')
    else if (name === 'worker.env') roles.add('worker')
    else if (name === 'admin.env') roles.add('admin')
    else {
      const match = /^relay-([A-Za-z0-9_-]+)\.env$/.exec(name)
      if (match) {
        roles.add('relays')
        relayInstances.push(match[1])
      }
    }
  }
  return { roles: sortRoles(roles), relayInstances: relayInstances.sort() }
}
