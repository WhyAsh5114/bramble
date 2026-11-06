// Minimal flag parser shared by the three CLIs: `--name value`, `--name=value`
// and bare `--flag`. Unknown flags are errors, so a typo never runs silently.

export interface FlagSpec {
  /** Flags that take a value. */
  value: string[]
  /** Flags that are switches. */
  bool: string[]
}

export interface ParsedFlags {
  values: Map<string, string>
  bools: Set<string>
}

export function parseFlags(argv: string[], spec: FlagSpec): ParsedFlags {
  const values = new Map<string, string>()
  const bools = new Set<string>()
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]
    if (arg === '--') continue
    if (!arg.startsWith('--')) throw new Error(`unexpected argument "${arg}"`)
    const eq = arg.indexOf('=')
    const name = eq === -1 ? arg.slice(2) : arg.slice(2, eq)
    if (spec.bool.includes(name)) {
      if (eq !== -1) throw new Error(`--${name} takes no value`)
      bools.add(name)
    } else if (spec.value.includes(name)) {
      const value = eq === -1 ? argv[++i] : arg.slice(eq + 1)
      if (value === undefined || value.startsWith('--')) throw new Error(`--${name} needs a value`)
      values.set(name, value)
    } else {
      throw new Error(`unknown flag --${name}`)
    }
  }
  return { values, bools }
}
