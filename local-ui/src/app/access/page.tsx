'use client'

// Grants (ADR 0005): this device, as a granter, computes a digest that binds
// requester + gateway + service; the admin wallet writes it into the
// requester's acl record. Gateways that trust this device as a granter accept it.
import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../../components/ui/card'
import { Input } from '../../components/ui/input'
import { Label } from '../../components/ui/label'
import { useApp } from '../../components/app-state'
import { Empty, NeedsTailnet, PageHeader, Problem, Section, TxSteps, errorText } from '../../components/bits'
import { api, type Device } from '../../lib/api'
import * as tx from '../../lib/chain/tx'
import { runSteps, useWallet, type TxStep } from '../../lib/wallet'

interface Grant {
  requester: string
  gateway: string
  service: string
  digest: string
}

/** Service names this device knows: its own, and what peers list to it. */
function useServiceNames(devices: Device[] | undefined) {
  const { state } = useApp()
  const [names, setNames] = useState<Record<string, string[]>>({})
  const peers =
    devices
      ?.filter((d) => !d.self && d.connected)
      .map((d) => d.label)
      .join(',') ?? ''
  useEffect(() => {
    let stop = false
    for (const label of peers.split(',').filter(Boolean))
      api
        .peerServices(label)
        .then((s) => !stop && setNames((n) => ({ ...n, [label]: s.map((x) => x.name) })))
        .catch(() => {})
    return () => {
      stop = true
    }
  }, [peers])
  return useMemo(() => {
    const out: Record<string, string[]> = { ...names }
    if (state?.label) out[state.label] = state.services.map((s) => s.name)
    return out
  }, [names, state?.label, state?.services])
}

function useGrants(devices: Device[] | undefined, services: Record<string, string[]>, extra: string[]) {
  const [grants, setGrants] = useState<Grant[]>()
  const [unknown, setUnknown] = useState<Record<string, number>>({})
  const key = JSON.stringify([devices?.map((d) => [d.label, d.acl, d.pubkey]), services, extra])
  useEffect(() => {
    if (!devices) return
    let stop = false
    ;(async () => {
      const found: Grant[] = []
      const left: Record<string, number> = {}
      const allServices = [...new Set([...Object.values(services).flat(), ...extra])]
      for (const d of devices) {
        const acl = new Set(d.acl ?? [])
        if (!acl.size) continue
        for (const g of devices) {
          if (g.label === d.label || !g.pubkey) continue
          for (const s of allServices) {
            const r = await api.grantDigest(d.label, g.label, s).catch(() => undefined)
            if (r && acl.has(r.digest)) {
              found.push({ requester: d.label, gateway: g.label, service: s, digest: r.digest })
              acl.delete(r.digest)
            }
          }
        }
        if (acl.size) left[d.label] = acl.size
      }
      if (!stop) {
        setGrants(found)
        setUnknown(left)
      }
    })()
    return () => {
      stop = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])
  return { grants, unknown }
}

const EXTRA_KEY = 'bramble-granted-services'
function loadExtra(): string[] {
  try {
    return JSON.parse(localStorage.getItem(EXTRA_KEY) ?? '[]')
  } catch {
    return []
  }
}

function AccessView() {
  const { state, devices } = useApp()
  const w = useWallet()
  const services = useServiceNames(devices)
  const [extra, setExtra] = useState<string[]>([])
  useEffect(() => setExtra(loadExtra()), [])
  const { grants, unknown } = useGrants(devices, services, extra)
  const live = devices?.filter((d) => d.pubkey && !d.revoked) ?? []
  const [requester, setRequester] = useState('')
  const [gateway, setGateway] = useState('')
  const [service, setService] = useState('')
  const [steps, setSteps] = useState<TxStep[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const tn = state?.tailnet
  const gw = devices?.find((d) => d.label === gateway)
  const trusts = !gw || !state?.label || (gw.granters ?? []).includes(state.label)
  const suggestions = [...new Set([...(services[gateway] ?? []), ...extra])].filter((s) => s !== 'ping')

  async function write(requesterLabel: string, digests: string[], label: string) {
    if (!tn) return
    setBusy(true)
    setError(undefined)
    try {
      await runSteps(
        [
          {
            label,
            run: () =>
              w.send(
                tx.setACL({ name: tn.name, registry: tn.registry, resolver: tn.resolver }, requesterLabel, digests)
              ),
          },
        ],
        setSteps
      )
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  async function grant(e: FormEvent) {
    e.preventDefault()
    const r = devices?.find((d) => d.label === requester)
    if (!r) return
    try {
      const { digest } = await api.grantDigest(requester, gateway, service)
      if (!extra.includes(service)) {
        const next = [...extra, service]
        setExtra(next)
        try {
          localStorage.setItem(EXTRA_KEY, JSON.stringify(next))
        } catch {}
      }
      await write(requester, [...(r.acl ?? []), digest], `Grant ${requester} → ${service} on ${gateway}`)
    } catch (err) {
      setError(errorText(err))
    }
  }

  return (
    <>
      {state?.phase === 'member' && (
        <Card className="mb-8 gap-4">
          <CardHeader>
            <div>
              <CardTitle>Grant access</CardTitle>
              <CardDescription className="mt-1.5">
                Open services are reachable by every member. A private service needs a grant: this device vouches for
                the requester, and your admin wallet writes it on-chain.
              </CardDescription>
            </div>
            {!w.admin && <Badge variant="outline">Admin wallet needed</Badge>}
          </CardHeader>
          <CardContent>
            <form onSubmit={grant} className="grid gap-4 sm:grid-cols-3">
              <div className="grid gap-1.5">
                <Label htmlFor="g-req">Device</Label>
                <select id="g-req" className="field" value={requester} onChange={(e) => setRequester(e.target.value)}>
                  <option value="">Choose…</option>
                  {live.map((d) => (
                    <option key={d.label}>{d.label}</option>
                  ))}
                </select>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="g-gw">May reach, on</Label>
                <select id="g-gw" className="field" value={gateway} onChange={(e) => setGateway(e.target.value)}>
                  <option value="">Choose…</option>
                  {live
                    .filter((d) => d.label !== requester)
                    .map((d) => (
                      <option key={d.label}>{d.label}</option>
                    ))}
                </select>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="g-svc">Service</Label>
                <Input
                  id="g-svc"
                  className="mono"
                  list="g-svc-list"
                  value={service}
                  onChange={(e) => setService(e.target.value.trim())}
                  placeholder="web"
                />
                <datalist id="g-svc-list">
                  {suggestions.map((s) => (
                    <option key={s} value={s} />
                  ))}
                </datalist>
              </div>
              <div className="sm:col-span-3">
                {!trusts && (
                  <p className="notice mb-3 text-xs">
                    {gateway} does not list {state?.label} as a granter, so it will ignore this grant.
                  </p>
                )}
                <Button type="submit" disabled={!requester || !gateway || !service || busy || !w.admin}>
                  {busy ? 'Working…' : 'Grant'}
                </Button>
              </div>
            </form>
            <TxSteps steps={steps} />
            {error && (
              <div className="mt-3">
                <Problem>{error}</Problem>
              </div>
            )}
          </CardContent>
        </Card>
      )}
      <Section id="grants" title="Active grants" hint="Decoded by this device; grants by other granters stay opaque">
        {!grants ? (
          <Empty>Checking grants…</Empty>
        ) : grants.length === 0 && !Object.keys(unknown).length ? (
          <Empty>No grants yet. Open services need none.</Empty>
        ) : (
          <Card className="gap-0 overflow-x-auto py-0">
            <table className="data-table">
              <thead>
                <tr>
                  <th>Device</th>
                  <th>Service</th>
                  <th>Gateway</th>
                  <th className="text-right">Action</th>
                </tr>
              </thead>
              <tbody>
                {grants.map((g) => (
                  <tr key={g.digest}>
                    <td className="mono">{g.requester}</td>
                    <td className="mono">{g.service}</td>
                    <td className="mono">{g.gateway}</td>
                    <td className="text-right">
                      {w.admin && (
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={busy}
                          onClick={() => {
                            const d = devices?.find((x) => x.label === g.requester)
                            void write(
                              g.requester,
                              (d?.acl ?? []).filter((x) => x !== g.digest),
                              `Remove ${g.requester} → ${g.service} on ${g.gateway}`
                            )
                          }}
                        >
                          Remove
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
                {Object.entries(unknown).map(([label, n]) => (
                  <tr key={label}>
                    <td className="mono">{label}</td>
                    <td colSpan={3} className="text-xs text-muted-foreground">
                      {n} more {n === 1 ? 'grant' : 'grants'} from another granter, or for a service this device does
                      not know.
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        )}
      </Section>
    </>
  )
}

export default function AccessPage() {
  return (
    <>
      <PageHeader title="Access" description="Who may reach which private service, enforced by each gateway." />
      <NeedsTailnet>
        <AccessView />
      </NeedsTailnet>
    </>
  )
}
