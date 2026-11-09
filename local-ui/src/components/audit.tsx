'use client'

import { useEffect, useState } from 'react'
import { ExternalLink } from 'lucide-react'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Card } from './ui/card'
import { CopyButton, Empty, Problem, TxSteps, errorText } from './bits'
import { useApp } from './app-state'
import { api, type AuditInfo, type Opening, type VerifyResult } from '../lib/api'
import * as tx from '../lib/chain/tx'
import { runSteps, useWallet, type TxStep } from '../lib/wallet'

const HASHSCAN = 'https://hashscan.io/testnet'

function status(a: AuditInfo) {
  if (!a.enabled) return <Badge variant="outline">Off</Badge>
  if (!a.topic) return <Badge variant="warning">No tailnet topic</Badge>
  if (!a.funded) return <Badge variant="warning">Needs HBAR</Badge>
  return a.active ? <Badge variant="success">Logging</Badge> : <Badge variant="outline">Starting</Badge>
}

function Verify() {
  const [text, setText] = useState('')
  const [result, setResult] = useState<VerifyResult>()
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)

  async function run() {
    setBusy(true)
    setError(undefined)
    setResult(undefined)
    try {
      setResult(await api.verifyOpening(JSON.parse(text) as Opening))
    } catch (e) {
      setError(e instanceof SyntaxError ? 'That is not JSON. Paste an opening exactly as it was copied.' : errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid gap-2">
      <h3 className="eyebrow">Check an opening</h3>
      <p className="m-0 text-xs text-muted-foreground">
        A gateway&apos;s owner can hand you an opening. Paste it to see whether the tailnet&apos;s topic holds the
        matching commitment, and when.
      </p>
      <textarea
        aria-label="Opening"
        className="mono min-h-20 w-full rounded-lg border border-input bg-card p-2 text-xs"
        value={text}
        onChange={(e) => setText(e.target.value)}
        spellCheck={false}
        placeholder='{"v":1,"event":{…},"salt":"…"}'
      />
      <div>
        <Button variant="outline" onClick={() => void run()} disabled={busy || !text.trim()}>
          {busy ? 'Checking…' : 'Check'}
        </Button>
      </div>
      {result && (
        <p className={`m-0 text-sm ${result.match ? 'text-success-strong' : 'text-destructive'}`}>
          {result.match
            ? `Match: entry ${result.sequence} on topic ${result.topic}, consensus time ${result.consensusTimestamp}.`
            : `No match on topic ${result.topic} (newest 500 entries searched).`}
        </p>
      )}
      {error && <Problem>{error}</Problem>}
    </div>
  )
}

/** Admin only: make the tailnet's topic with this device's key, then publish it as a tailnet record. */
function TopicSetup({ info, onDone }: { info: AuditInfo; onDone: () => void }) {
  const { state } = useApp()
  const w = useWallet()
  const [steps, setSteps] = useState<TxStep[]>([])
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const tn = state?.tailnet
  if (!tn || !w.admin) return null

  async function create() {
    if (!tn) return
    setBusy(true)
    setError(undefined)
    let topic = ''
    try {
      await runSteps(
        [
          {
            label: 'Create the audit topic with this device’s Hedera key',
            run: async () => {
              topic = (await api.createAuditTopic()).topic
              return undefined
            },
          },
          {
            label: 'Publish it as the tailnet’s audit-topic record',
            run: () =>
              w.send(tx.setTailnetAuditTopic({ name: tn.name, registry: tn.registry, resolver: tn.resolver }, topic)),
          },
        ],
        setSteps
      )
      onDone()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  async function clear() {
    if (!tn) return
    setBusy(true)
    setError(undefined)
    try {
      await runSteps(
        [
          {
            label: 'Clear the tailnet’s audit-topic record',
            run: () =>
              w.send(tx.setTailnetAuditTopic({ name: tn.name, registry: tn.registry, resolver: tn.resolver }, '')),
          },
        ],
        setSteps
      )
      onDone()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid gap-2">
      <h3 className="eyebrow">Tailnet admin</h3>
      <p className="m-0 text-xs text-muted-foreground">
        {info.topic
          ? 'Gateways that opted in write to this topic. Clearing the record stops them without a restart.'
          : 'Turning on the audit log for the tailnet takes two steps: this device creates a topic (it needs a little test HBAR), then your wallet publishes it as a tailnet record.'}
      </p>
      <div className="flex flex-wrap gap-2">
        {info.topic ? (
          <Button variant="outline" onClick={() => void clear()} disabled={busy}>
            Clear the tailnet topic
          </Button>
        ) : (
          <Button onClick={() => void create()} disabled={busy || !info.funded || !info.enabled}>
            {busy ? 'Working…' : 'Create and publish a topic'}
          </Button>
        )}
      </div>
      <TxSteps steps={steps} />
      {error && <Problem>{error}</Problem>}
    </div>
  )
}

/** Opt-in: commit to this gateway's access decisions on the tailnet's HCS topic (ADR 0021). */
export function AuditCard() {
  const [info, setInfo] = useState<AuditInfo>()
  const [openings, setOpenings] = useState<Opening[]>([])
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let live = true
    const load = () => {
      api
        .audit()
        .then((a) => live && setInfo(a))
        .catch((e) => live && setError(errorText(e)))
      api
        .auditOpenings()
        .then((o) => live && setOpenings(o))
        .catch(() => undefined)
    }
    load()
    const t = setInterval(load, 8_000)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [tick])

  async function toggle(enabled: boolean) {
    setBusy(true)
    setError(undefined)
    try {
      setInfo(await api.setAudit(enabled))
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  if (!info) return <Card className="px-5">{error ? <Problem>{error}</Problem> : <Empty>Loading…</Empty>}</Card>
  return (
    <Card className="gap-4 px-5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="m-0 max-w-prose text-sm text-muted-foreground">
          When another device asks this one for a service, the allow or deny can be written to a Hedera topic as a
          salted commitment. The topic shows that a decision happened and when. It does not show who asked, for what, or
          the outcome; you keep that and can open it to an auditor. It records decisions; it does not enforce them, and
          it cannot prove nothing was left out. Each entry costs a fraction of a cent of test HBAR, paid from this
          device&apos;s Hedera address (see Paying relays).
        </p>
        {status(info)}
      </div>
      <dl className="m-0 grid gap-2 text-sm">
        <div className="grid gap-0.5 sm:grid-cols-[200px_minmax(0,1fr)] sm:gap-4">
          <dt className="eyebrow pt-0.5">Tailnet topic</dt>
          <dd className="m-0">
            {info.topic ? (
              <a
                className="mono inline-flex items-center gap-1 underline"
                href={`${HASHSCAN}/topic/${info.topic}`}
                target="_blank"
                rel="noreferrer"
              >
                {info.topic}
                <ExternalLink className="size-3" aria-hidden="true" />
              </a>
            ) : (
              <span className="text-muted-foreground">None. The tailnet&apos;s admin sets it below.</span>
            )}
          </dd>
        </div>
        {info.active && (
          <div className="grid gap-0.5 sm:grid-cols-[200px_minmax(0,1fr)] sm:gap-4">
            <dt className="eyebrow pt-0.5">Written</dt>
            <dd className="m-0 tabular-nums">
              {info.posted} posted · {info.dropped} skipped by the rate limit · {info.failed} failed
            </dd>
          </div>
        )}
      </dl>
      <div className="flex flex-wrap items-center gap-3">
        {info.enabled ? (
          <Button variant="outline" onClick={() => void toggle(false)} disabled={busy}>
            Turn off
          </Button>
        ) : (
          <Button onClick={() => void toggle(true)} disabled={busy}>
            Turn on the audit log
          </Button>
        )}
      </div>
      {(error || info.error) && <Problem>{error ?? info.error}</Problem>}

      <TopicSetup info={info} onDone={() => setTimeout(() => setTick((t) => t + 1), 20_000)} />

      {openings.length > 0 && (
        <div className="grid gap-2">
          <h3 className="eyebrow">Openings kept on this device</h3>
          <p className="m-0 text-xs text-muted-foreground">
            Held in memory only, newest {openings.length}; a restart forgets them, and then the topic entries can no
            longer be opened.
          </p>
          <table className="data-table">
            <thead>
              <tr>
                <th>Decision</th>
                <th>Device</th>
                <th>Service</th>
                <th>Opening</th>
              </tr>
            </thead>
            <tbody>
              {[...openings]
                .reverse()
                .slice(0, 20)
                .map((o) => (
                  <tr key={o.c ?? o.salt}>
                    <td>
                      <Badge variant={o.event.outcome === 'allowed' ? 'success' : 'destructive'}>
                        {o.event.outcome}
                      </Badge>
                    </td>
                    <td className="mono">{o.event.requester}</td>
                    <td className="mono">{o.event.service}</td>
                    <td>
                      <CopyButton value={JSON.stringify(o)} label="Copy" />
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      )}
      <Verify />
    </Card>
  )
}
