// Builds the preflight context for this machine from the process env, the repo
// `.env`, the seeded /etc/bramble env files, and the DEMO_* connection settings.
import { execFile } from 'node:child_process'
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { GRANTER_KEY_RING_PATH } from '../../../admincli/src/granter-key-path'
import { listDir, readEnvFile } from './env-file'
import { httpGet, makeNodeProbe, tcpConnect, type ProbeSpec } from './probe'
import { secretValues } from './redact'
import type { Env, NodeProbe, PreflightContext } from './preflight'
import { ENV_DIR, NODE_ROLES, rolesFromEnvFileNames, sortRoles, type NodeRole, type Role } from './topology'

const here = path.dirname(fileURLToPath(import.meta.url))
export const REPO_ENV_PATH = path.resolve(here, '../../../.env')

/** Process env wins over the `.env` file, as in admincli/src/setup.ts. */
export function loadEnv(processEnv: Env, envFile: Env | undefined): Env {
  return { ...(envFile ?? {}), ...Object.fromEntries(Object.entries(processEnv).filter(([, v]) => v !== undefined)) }
}

export function natBoxEnabled(env: Env): boolean {
  return env.DEMO_NAT_BOX === '1' || env.DEMO_NAT_BOX === 'true'
}

export function natBoxNamespace(env: Env): string {
  return env.NATBOX_NS || 'bramble-node'
}

/** How to reach each node role: this machine when it runs the role, else DEMO_<ROLE>_SSH. */
export function probeSpecs(env: Env, localRoles: Role[]): Partial<Record<NodeRole, ProbeSpec>> {
  const specs: Partial<Record<NodeRole, ProbeSpec>> = {}
  for (const role of NODE_ROLES) {
    const upper = role.toUpperCase()
    const ssh = env[`DEMO_${upper}_SSH`]?.trim()
    const netns = env[`DEMO_${upper}_NETNS`]?.trim()
    if (localRoles.includes(role)) {
      specs[role] = natBoxEnabled(env) ? { netns: natBoxNamespace(env) } : {}
    } else if (ssh) {
      specs[role] = { ssh, ...(netns ? { netns } : {}) }
    }
  }
  return specs
}

/**
 * Every secret value this machine's env knows: the process env, the repo `.env`
 * and every file under /etc/bramble. Used to scrub text before it is printed.
 */
export function ambientSecrets(processEnv: Env, envDir = ENV_DIR): string[] {
  const files = listDir(envDir)
    .filter((name) => name.endsWith('.env'))
    .map((name) => readEnvFile(path.join(envDir, name)))
  return secretValues(processEnv, readEnvFile(REPO_ENV_PATH), ...files)
}

/** True when systemd reports the unit active on this machine. */
export function localUnitActive(unit: string): Promise<boolean> {
  return new Promise((resolve) => {
    execFile('systemctl', ['is-active', '--quiet', unit], (err) => resolve(!err))
  })
}

export interface Detected {
  roles: Role[]
  relayInstances: string[]
}

/** Roles this machine runs, from the env files the installer seeded under /etc/bramble. */
export function detectRoles(dir = ENV_DIR): Detected {
  const found = rolesFromEnvFileNames(listDir(dir))
  return { roles: found.roles, relayInstances: found.relayInstances }
}

export function buildContext(opts: {
  processEnv: Env
  localRoles: Role[]
  relayInstances: string[]
  envDir?: string
}): PreflightContext {
  const env = loadEnv(opts.processEnv, readEnvFile(REPO_ENV_PATH))
  const dir = opts.envDir ?? ENV_DIR
  const roleEnv: Partial<Record<Role, Env>> = {}
  for (const role of sortRoles(opts.localRoles)) {
    const file = role === 'relays' ? `relay-${opts.relayInstances[0] ?? 'relay1'}.env` : `${role}.env`
    const fromFile = readEnvFile(path.join(dir, file))
    if (fromFile) roleEnv[role] = { ...env, ...fromFile }
  }
  const nodes: Partial<Record<NodeRole, NodeProbe>> = {}
  for (const [role, spec] of Object.entries(probeSpecs(env, opts.localRoles)) as Array<[NodeRole, ProbeSpec]>) {
    nodes[role] = makeNodeProbe(spec, role)
  }
  return {
    env,
    localRoles: sortRoles(opts.localRoles),
    roleEnv,
    nodes,
    http: httpGet,
    tcp: tcpConnect,
    granterRingPresent: existsSync(GRANTER_KEY_RING_PATH),
    secrets: ambientSecrets(opts.processEnv, dir),
    unitActive: localUnitActive,
  }
}
