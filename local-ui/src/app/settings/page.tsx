'use client'

import { useEffect, useState } from 'react'
import { Button } from '../../components/ui/button'
import { Card } from '../../components/ui/card'
import { Label } from '../../components/ui/label'
import { useApp } from '../../components/app-state'
import { CopyButton, PageHeader, Problem, Section, errorText } from '../../components/bits'
import { AuditCard } from '../../components/audit'
import { PayCard } from '../../components/pay'
import { LeaveCard } from '../../components/setup'
import { api } from '../../lib/api'

function Row({ k, v, mono = true }: { k: string; v?: React.ReactNode; mono?: boolean }) {
  return (
    <div className="grid gap-0.5 sm:grid-cols-[200px_minmax(0,1fr)] sm:gap-4">
      <dt className="eyebrow pt-0.5">{k}</dt>
      <dd className={`m-0 text-sm break-all ${mono ? 'mono' : ''}`}>{v ?? '—'}</dd>
    </div>
  )
}

export default function SettingsPage() {
  const { state, refresh } = useApp()
  const [relayFirst, setRelayFirst] = useState(false)
  const [rpc, setRpc] = useState('')
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string>()
  const loaded = !!state
  useEffect(() => {
    if (!state) return
    setRelayFirst(state.settings.relayFirst)
    setRpc(state.settings.rpcUrls.join('\n'))
    // Load once; later polls must not overwrite what the user is typing.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded])
  if (!state) return <PageHeader title="Settings" />

  async function save() {
    setError(undefined)
    setSaved(false)
    try {
      const urls = rpc
        .split(/\s+/)
        .map((s) => s.trim())
        .filter(Boolean)
      await api.setSettings(relayFirst, urls)
      await refresh()
      setSaved(true)
    } catch (e) {
      setError(errorText(e))
    }
  }

  const n = state.network
  return (
    <>
      <PageHeader title="Settings" description="This device's identity, connection preferences and chain access." />
      <Section id="identity" title="Identity">
        <Card className="gap-3 px-5">
          <dl className="m-0 grid gap-3">
            <Row k="Device" v={state.label ? `${state.label}.${state.tailnet?.name}` : 'not on a tailnet'} />
            <Row k="Key fingerprint" v={state.fingerprint} />
            <Row
              k="Public key"
              v={
                <span className="flex flex-wrap items-center gap-2">
                  <span>{state.publicKey}</span>
                  <CopyButton value={state.publicKey} />
                </span>
              }
            />
            <Row k="Version" v={state.version} />
          </dl>
        </Card>
      </Section>
      <Section id="conn" title="Connection">
        <Card className="gap-4 px-5">
          <label className="flex items-start gap-3 text-sm">
            <input
              type="checkbox"
              className="mt-1"
              checked={relayFirst}
              onChange={(e) => setRelayFirst(e.target.checked)}
            />
            <span>
              <span className="font-medium">Relay first</span>
              <span className="block text-xs text-muted-foreground">
                Start every link through a data relay, then try to go direct. Useful on networks that block UDP hole
                punching. The relay still sees only encrypted packets.
              </span>
            </span>
          </label>
          <div className="grid gap-1.5">
            <Label htmlFor="rpc">Sepolia RPC endpoints</Label>
            <textarea
              id="rpc"
              className="mono min-h-24 w-full rounded-lg border border-input bg-card p-2 text-xs"
              value={rpc}
              onChange={(e) => setRpc(e.target.value)}
              placeholder="One URL per line. Empty uses the built-in public endpoints."
              spellCheck={false}
            />
            <p className="m-0 text-xs text-muted-foreground">
              In use: {state.chain?.endpoints.join(', ') || 'built-in public endpoints'}. Avoid URLs with API keys if
              others use this machine; they are stored in this device&apos;s state file.
            </p>
          </div>
          <div className="flex items-center gap-3">
            <Button onClick={() => void save()}>Save</Button>
            {saved && <span className="text-xs text-success-strong">Saved. The daemon applies it now.</span>}
          </div>
          {error && <Problem>{error}</Problem>}
        </Card>
      </Section>
      <Section id="pay" title="Paying relays" hint="Optional, Hedera testnet">
        <PayCard />
      </Section>
      <Section id="audit" title="Audit log" hint="Optional, Hedera testnet">
        <AuditCard />
      </Section>
      <Section id="chain" title="Chain">
        <Card className="gap-3 px-5">
          <dl className="m-0 grid gap-3">
            <Row k="Network" v={`Sepolia (${n.chainId})`} />
            <Row k="Tailnet registry" v={state.tailnet?.registry} />
            <Row k="Tailnet resolver" v={state.tailnet?.resolver} />
            <Row k="Tailnet owner" v={state.tailnet?.owner} />
            <Row
              k="Synced to block"
              v={state.chain ? `${state.chain.confirmed} (head ${state.chain.head})` : undefined}
            />
            <Row k="Tailnet directory" v={`${n.netSuffix} · ${n.netRegistry}`} />
            <Row k="Relay directory" v={`${n.relaySuffix} · ${n.relayRegistry}`} />
          </dl>
          {state.chain?.error && <Problem>{state.chain.error}</Problem>}
        </Card>
      </Section>
      <LeaveCard />
    </>
  )
}
