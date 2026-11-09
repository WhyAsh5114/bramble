'use client'

import { useEffect, useState, type FormEvent } from 'react'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../../components/ui/card'
import { Input } from '../../components/ui/input'
import { Label } from '../../components/ui/label'
import { useApp } from '../../components/app-state'
import { CopyButton, Empty, PageHeader, Problem, Section, TxSteps, errorText } from '../../components/bits'
import { api, bytes, type RelayInfo } from '../../lib/api'
import * as tx from '../../lib/chain/tx'
import { runSteps, useWallet, type TxStep } from '../../lib/wallet'

const RUN = 'bramble relay -public-host <public IP>   # open 9420/tcp, 3478/udp, 40000-40999/udp'

function Budget({ r }: { r: RelayInfo }) {
  const b = r.status?.budget
  if (!b) return <span className="text-muted-foreground">—</span>
  if (b.forwardingPaused) return <Badge variant="warning">Budget used up</Badge>
  return (
    <span className="text-xs text-muted-foreground">
      {bytes(b.dayBytes)} of {bytes(b.dayLimit)} today
    </span>
  )
}

function Directory({ relays, onSaved }: { relays?: RelayInfo[]; onSaved: () => void }) {
  const { state } = useApp()
  const w = useWallet()
  const [chosen, setChosen] = useState<string[]>()
  const [steps, setSteps] = useState<TxStep[]>([])
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const preferred = relays?.filter((r) => r.preferred).map((r) => r.label) ?? []
  const picks = chosen ?? preferred
  const tn = state?.tailnet
  const editable = !!tn && w.admin

  async function save() {
    if (!tn) return
    setBusy(true)
    setError(undefined)
    try {
      await runSteps(
        [
          {
            label: `Set the tailnet relay list to ${picks.join(', ') || '(directory default)'}`,
            run: () =>
              w.send(tx.setTailnetRelays({ name: tn.name, registry: tn.registry, resolver: tn.resolver }, picks)),
          },
        ],
        setSteps
      )
      setChosen(undefined)
      onSaved()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  if (!relays) return <Empty>Probing the relay directory…</Empty>
  if (relays.length === 0) return <Empty>The directory is empty.</Empty>
  return (
    <>
      <Card className="gap-0 overflow-x-auto py-0">
        <table className="data-table">
          <thead>
            <tr>
              {editable && <th>Use</th>}
              <th>Relay</th>
              <th>Region</th>
              <th>Carries traffic</th>
              <th>Round trip</th>
              <th>Today</th>
            </tr>
          </thead>
          <tbody>
            {relays.map((r) => (
              <tr key={r.label}>
                {editable && (
                  <td>
                    <input
                      type="checkbox"
                      aria-label={`Prefer ${r.label}`}
                      checked={picks.includes(r.label)}
                      onChange={(e) =>
                        setChosen(e.target.checked ? [...picks, r.label] : picks.filter((x) => x !== r.label))
                      }
                    />
                  </td>
                )}
                <td>
                  <span className="mono font-medium">{r.label}</span>
                  {r.preferred && (
                    <Badge variant="success" className="ml-2">
                      tailnet relay
                    </Badge>
                  )}
                  <span className="mono block text-xs text-muted-foreground">{r.address}</span>
                </td>
                <td>{r.region || '—'}</td>
                <td>{r.dataRelay ? 'Yes' : 'Introductions only'}</td>
                <td>
                  {r.error ? (
                    <span className="block max-w-56 truncate text-xs text-destructive" title={r.error}>
                      {r.error}
                    </span>
                  ) : (
                    <span className="mono">{r.millis ?? '—'} ms</span>
                  )}
                </td>
                <td>
                  <Budget r={r} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
      {editable && (
        <div className="mt-3 flex flex-wrap items-center gap-3">
          <Button onClick={() => void save()} disabled={busy || chosen === undefined}>
            {busy ? 'Working…' : 'Save tailnet relays'}
          </Button>
          <span className="text-xs text-muted-foreground">
            Devices try the tailnet&apos;s relays first, then the three oldest directory relays.
          </span>
        </div>
      )}
      <TxSteps steps={steps} />
      {error && (
        <div className="mt-3">
          <Problem>{error}</Problem>
        </div>
      )}
    </>
  )
}

function Publish() {
  const { state } = useApp()
  const w = useWallet()
  const [f, setF] = useState({ label: '', rendezvous: '', pin: '', stun: '', region: '', data: true })
  const [steps, setSteps] = useState<TxStep[]>([])
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const net = state?.network
  const valid = tx.LABEL_RE.test(f.label) && /^[^\s:]+:\d+$/.test(f.rendezvous) && f.pin.startsWith('sha256/')

  async function publish(e: FormEvent) {
    e.preventDefault()
    if (!net) return
    setBusy(true)
    setError(undefined)
    let resolver: `0x${string}` | undefined
    try {
      await runSteps(
        [
          {
            label: `Claim ${f.label}.${net.relaySuffix}`,
            run: async () => {
              const r = await w.send(tx.claimRelay(net.relayRegistrar, f.label))
              resolver = tx.claimedFromReceipt(r).resolver
              return r
            },
          },
          {
            label: 'Publish its address and pin',
            run: () =>
              w.send(
                tx.writeRelayRecords(resolver!, net.relaySuffix, f.label, {
                  rendezvous: f.rendezvous,
                  pin: f.pin,
                  dataRelay: f.data,
                  stun: f.stun || undefined,
                  region: f.region || undefined,
                })
              ),
          },
        ],
        setSteps
      )
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setF({ ...f, [k]: k === 'data' ? e.target.checked : e.target.value.trim() })

  return (
    <Card className="gap-4">
      <CardHeader>
        <div>
          <CardTitle>Run a relay</CardTitle>
          <CardDescription className="mt-1.5">
            Anyone can add a free relay to the directory. Relays see only sealed introductions and encrypted WireGuard
            packets; devices pin each relay&apos;s TLS key and drop relays that misbehave.
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent className="grid gap-4">
        <div>
          <p className="m-0 mb-2 text-sm">1. Start it on a host with a public address. It prints its pin on start.</p>
          <pre className="mono m-0 overflow-x-auto rounded-md bg-secondary p-3 text-xs">{RUN}</pre>
          <div className="mt-2">
            <CopyButton value={RUN} label="Copy command" />
          </div>
        </div>
        <form onSubmit={publish} className="grid gap-3">
          <p className="m-0 text-sm">2. Publish it with your wallet (two transactions).</p>
          <div className="grid gap-3 sm:grid-cols-2">
            {(
              [
                ['label', 'Name', 'my-relay'],
                ['rendezvous', 'Rendezvous address', '203.0.113.7:9420'],
                ['pin', 'TLS pin', 'sha256/…'],
                ['stun', 'STUN address (optional)', '203.0.113.7:3478'],
                ['region', 'Region (optional)', 'eu-west'],
              ] as const
            ).map(([k, label, ph]) => (
              <div key={k} className="grid gap-1.5">
                <Label htmlFor={`relay-${k}`}>{label}</Label>
                <Input id={`relay-${k}`} className="mono" value={f[k]} onChange={set(k)} placeholder={ph} />
              </div>
            ))}
            <label className="flex items-center gap-2 self-end pb-2 text-sm">
              <input type="checkbox" checked={f.data} onChange={set('data')} /> Carry traffic (data relay)
            </label>
          </div>
          <div>
            <Button type="submit" disabled={!valid || busy || !w.available}>
              {busy ? 'Working…' : 'Publish relay'}
            </Button>
          </div>
        </form>
        <TxSteps steps={steps} />
        {error && <Problem>{error}</Problem>}
      </CardContent>
    </Card>
  )
}

export default function RelaysPage() {
  const [relays, setRelays] = useState<RelayInfo[]>()
  const [tick, setTick] = useState(0)
  useEffect(() => {
    api
      .relays()
      .then(setRelays)
      .catch(() => setRelays([]))
  }, [tick])
  return (
    <>
      <PageHeader
        title="Relays"
        description="The open relay directory under relay.bramble.eth, probed from this device."
      />
      <Section id="dir" title="Directory" hint="Showing the 30 oldest entries">
        <Directory relays={relays} onSaved={() => setTimeout(() => setTick((t) => t + 1), 31_000)} />
      </Section>
      <Publish />
    </>
  )
}
