'use client'

import { useEffect, useState } from 'react'
import { ExternalLink } from 'lucide-react'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Card } from './ui/card'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { CopyButton, Empty, Problem, errorText } from './bits'
import { api, ago, bytes, usdc, type PayInfo } from '../lib/api'

const FAUCET = 'https://portal.hedera.com/faucet'
const CIRCLE = 'https://faucet.circle.com'
const HASHSCAN = 'https://hashscan.io/testnet'

/** Units are USDC's smallest unit; the form edits whole USDC. */
const toUnits = (v: string) => Math.round(parseFloat(v) * 1e6)
const toUsdc = (n: number) => String(n / 1e6)

function Link({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a href={href} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 underline">
      {children}
      <ExternalLink className="size-3" aria-hidden="true" />
    </a>
  )
}

function status(p: PayInfo) {
  if (!p.enabled) return <Badge variant="outline">Off</Badge>
  if (!p.account) return <Badge variant="warning">Waiting for funds</Badge>
  if (p.usdc === 0) return <Badge variant="warning">No USDC</Badge>
  return p.active ? <Badge variant="success">Paying</Badge> : <Badge variant="outline">Starting</Badge>
}

/** Opt-in: pay relays that charge for data sessions with this device's own Hedera testnet key (ADR 0021). */
export function PayCard() {
  const [info, setInfo] = useState<PayInfo>()
  const [session, setSession] = useState('')
  const [day, setDay] = useState('')
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    let live = true
    const load = () =>
      api
        .pay()
        .then((p) => live && setInfo(p))
        .catch((e) => live && setError(errorText(e)))
    void load()
    const t = setInterval(load, 8_000)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [])
  useEffect(() => {
    // Load the caps once; polling must not overwrite what is being typed.
    if (info && session === '' && day === '') {
      setSession(toUsdc(info.maxSession))
      setDay(toUsdc(info.maxDay))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [info?.enabled])

  async function apply(enabled: boolean) {
    setBusy(true)
    setError(undefined)
    setSaved(false)
    try {
      const s = toUnits(session)
      const d = toUnits(day)
      if (!Number.isFinite(s) || !Number.isFinite(d) || s <= 0 || d <= 0) throw new Error('Enter caps above zero.')
      setInfo(await api.setPay(enabled, s, d))
      setSaved(true)
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
          Free relays always come first. When none can serve, this device can pay a relay that charges, in testnet USDC
          on Hedera, within the caps below. The key is made on this device, is separate from your wallet, and cannot
          write tailnet records. Testnet tokens have no value.
        </p>
        {status(info)}
      </div>

      {info.enabled && (
        <dl className="m-0 grid gap-3 text-sm">
          <div className="grid gap-0.5 sm:grid-cols-[200px_minmax(0,1fr)] sm:gap-4">
            <dt className="eyebrow pt-0.5">Fund this address</dt>
            <dd className="m-0">
              <span className="mono mr-2 break-all">{info.evmAddress}</span>
              <CopyButton value={info.evmAddress ?? ''} />
              <span className="mt-1 block text-xs text-muted-foreground">
                Send test HBAR from the <Link href={FAUCET}>Hedera faucet</Link> to this address; that creates the
                account. Then send testnet USDC (token 0.0.429274) from <Link href={CIRCLE}>Circle&apos;s faucet</Link>{' '}
                by the same address. A relay&apos;s facilitator pays the network fee, so USDC is all a payment needs.
              </span>
            </dd>
          </div>
          <div className="grid gap-0.5 sm:grid-cols-[200px_minmax(0,1fr)] sm:gap-4">
            <dt className="eyebrow pt-0.5">Account</dt>
            <dd className="m-0">
              {info.account ? (
                <Link href={`${HASHSCAN}/account/${info.account}`}>{info.account}</Link>
              ) : (
                <span className="text-muted-foreground">Not created yet. It appears once the address is funded.</span>
              )}
            </dd>
          </div>
          <div className="grid gap-0.5 sm:grid-cols-[200px_minmax(0,1fr)] sm:gap-4">
            <dt className="eyebrow pt-0.5">Balance</dt>
            <dd className="m-0 tabular-nums">
              {usdc(info.usdc)} · {(info.hbarTinybar / 1e8).toLocaleString('en', { maximumFractionDigits: 4 })} HBAR
            </dd>
          </div>
          {info.active && (
            <div className="grid gap-0.5 sm:grid-cols-[200px_minmax(0,1fr)] sm:gap-4">
              <dt className="eyebrow pt-0.5">Spent today</dt>
              <dd className="m-0 tabular-nums">
                {usdc(info.spentToday)} of {usdc(info.maxDay)}
              </dd>
            </div>
          )}
          {info.keyFile && (
            <div className="grid gap-0.5 sm:grid-cols-[200px_minmax(0,1fr)] sm:gap-4">
              <dt className="eyebrow pt-0.5">Key file</dt>
              <dd className="mono m-0 break-all text-xs">{info.keyFile}</dd>
            </div>
          )}
        </dl>
      )}

      <div className="grid gap-3 sm:grid-cols-2">
        <div className="grid gap-1.5">
          <Label htmlFor="pay-session">Most per session (USDC)</Label>
          <Input
            id="pay-session"
            className="mono"
            inputMode="decimal"
            value={session}
            onChange={(e) => setSession(e.target.value)}
          />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="pay-day">Most per day (USDC)</Label>
          <Input
            id="pay-day"
            className="mono"
            inputMode="decimal"
            value={day}
            onChange={(e) => setDay(e.target.value)}
          />
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        {info.enabled ? (
          <>
            <Button onClick={() => void apply(true)} disabled={busy}>
              Save caps
            </Button>
            <Button variant="outline" onClick={() => void apply(false)} disabled={busy}>
              Turn off
            </Button>
          </>
        ) : (
          <Button onClick={() => void apply(true)} disabled={busy}>
            Turn on paying relays
          </Button>
        )}
        {saved && <span className="text-xs text-success-strong">Saved.</span>}
      </div>
      {(error || info.error) && <Problem>{error ?? info.error}</Problem>}

      {info.recent.length > 0 && (
        <div>
          <h3 className="eyebrow mb-2">Recent payments</h3>
          <table className="data-table">
            <thead>
              <tr>
                <th>When</th>
                <th>Relay</th>
                <th>Paid</th>
                <th>Buys</th>
                <th>Transaction</th>
              </tr>
            </thead>
            <tbody>
              {[...info.recent].reverse().map((p) => (
                <tr key={p.transaction}>
                  <td>{ago(p.at)}</td>
                  <td className="mono">{p.relay}</td>
                  <td className="tabular-nums">{usdc(p.amount)}</td>
                  <td className="tabular-nums">{bytes(p.bytes)}</td>
                  <td>
                    <Link href={`${HASHSCAN}/transaction/${p.transaction}`}>View</Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  )
}
