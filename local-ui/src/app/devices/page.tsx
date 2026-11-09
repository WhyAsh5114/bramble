'use client'

import { useState, type FormEvent } from 'react'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../../components/ui/card'
import { Label } from '../../components/ui/label'
import { useApp } from '../../components/app-state'
import { Empty, NeedsTailnet, PageHeader, PathBadge, Problem, Section, TxSteps, errorText } from '../../components/bits'
import { PingButton } from '../../components/ping'
import { api, type Device, type JoinRequest } from '../../lib/api'
import * as tx from '../../lib/chain/tx'
import { enrollSteps } from '../../lib/enroll'
import { runSteps, useWallet, type TxStep } from '../../lib/wallet'

function useTx() {
  const [steps, setSteps] = useState<TxStep[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const { refresh } = useApp()
  async function run(list: Parameters<typeof runSteps>[0]) {
    setBusy(true)
    setError(undefined)
    try {
      await runSteps(list, setSteps)
      await refresh()
      return true
    } catch (e) {
      setError(errorText(e))
      return false
    } finally {
      setBusy(false)
    }
  }
  return { steps, busy, error, run, reset: () => setSteps([]) }
}

function Enroll() {
  const { state } = useApp()
  const w = useWallet()
  const [code, setCode] = useState('')
  const [req, setReq] = useState<JoinRequest>()
  const [error, setError] = useState<string>()
  const t = useTx()
  const tn = state?.tailnet
  if (!tn || !state?.label) return null

  async function read(e: FormEvent) {
    e.preventDefault()
    setError(undefined)
    setReq(undefined)
    t.reset()
    try {
      setReq(await api.joinCode(code.trim()))
    } catch (err) {
      setError(errorText(err))
    }
  }
  async function enroll() {
    if (!req || !tn || !state?.label) return
    const owner = w.address ?? (await w.connect())
    if (!owner) return
    const ok = await t.run(
      enrollSteps(w.send, {
        tailnet: { name: tn.name, registry: tn.registry, resolver: tn.resolver },
        label: req.l,
        owner,
        pubkey: req.k,
        meshIp: req.meshIp,
        granters: [state.label],
        registered: !!req.existing,
      })
    )
    if (ok) {
      setCode('')
      setReq(undefined)
    }
  }

  return (
    <Card className="mb-8 gap-4">
      <CardHeader>
        <div>
          <CardTitle>Enroll a device</CardTitle>
          <CardDescription className="mt-1.5">
            Paste the join code the new device shows. Compare the fingerprint with its owner before you sign.
          </CardDescription>
        </div>
        {!w.admin && <Badge variant="outline">Admin wallet needed</Badge>}
      </CardHeader>
      <CardContent>
        <form onSubmit={read} className="grid gap-2">
          <Label htmlFor="join-code">Join code</Label>
          <textarea
            id="join-code"
            className="mono min-h-20 w-full rounded-lg border border-input bg-card p-2 text-xs"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            placeholder="bramble-join:1:…"
            spellCheck={false}
          />
          <div>
            <Button type="submit" variant="outline" disabled={!code.trim()}>
              Read code
            </Button>
          </div>
        </form>
        {error && (
          <div className="mt-4">
            <Problem>{error}</Problem>
          </div>
        )}
        {req && (
          <div className="mt-4 rounded-lg border border-divider p-4 text-sm">
            <div className="grid gap-3 sm:grid-cols-3">
              <div>
                <p className="eyebrow">Name</p>
                <p className="mono m-0 break-all">{req.name}</p>
              </div>
              <div>
                <p className="eyebrow">Fingerprint</p>
                <p className="mono m-0">{req.fingerprint}</p>
              </div>
              <div>
                <p className="eyebrow">Mesh address</p>
                <p className="mono m-0">{req.meshIp}</p>
              </div>
            </div>
            {req.existing && (
              <p className="notice mt-3 mb-0 text-xs">
                {req.existing.pubkey === req.k
                  ? 'This device is already enrolled with this key. Signing again rewrites the same records.'
                  : `The name ${req.l} is already registered${req.existing.pubkey ? ' with a different key' : ''}. Enrolling replaces its key.`}
              </p>
            )}
            <div className="mt-3 flex gap-2">
              <Button onClick={() => void enroll()} disabled={t.busy || !w.address || !w.admin}>
                {t.busy ? 'Working…' : req.existing ? 'Write records' : 'Register and enroll'}
              </Button>
              <Button variant="ghost" onClick={() => setReq(undefined)}>
                Cancel
              </Button>
            </div>
          </div>
        )}
        <TxSteps steps={t.steps} />
        {t.error && (
          <div className="mt-3">
            <Problem>{t.error}</Problem>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function RevokeButton({ device }: { device: Device }) {
  const { state } = useApp()
  const w = useWallet()
  const t = useTx()
  const tn = state?.tailnet
  if (!tn || !w.admin) return null
  return (
    <span className="grid justify-items-end gap-1">
      <Button
        variant={device.revoked ? 'outline' : 'destructive'}
        size="sm"
        disabled={t.busy}
        onClick={() =>
          void t.run([
            {
              label: device.revoked ? 'Restore' : 'Revoke',
              run: () =>
                w.send(
                  tx.setRevoked(
                    { name: tn.name, registry: tn.registry, resolver: tn.resolver },
                    device.label,
                    !device.revoked
                  )
                ),
            },
          ])
        }
      >
        {t.busy ? 'Signing…' : device.revoked ? 'Restore' : 'Revoke'}
      </Button>
      {t.error && (
        <span className="max-w-48 truncate text-xs text-destructive" title={t.error}>
          {t.error}
        </span>
      )}
    </span>
  )
}

function DeviceTable() {
  const { devices } = useApp()
  if (!devices) return <Empty>Loading devices…</Empty>
  if (devices.length === 0) return <Empty>No devices registered yet.</Empty>
  return (
    <Card className="gap-0 overflow-x-auto py-0">
      <table className="data-table">
        <thead>
          <tr>
            <th>Device</th>
            <th>Mesh address</th>
            <th>Status</th>
            <th>Fingerprint</th>
            <th>Expires</th>
            <th className="text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          {devices.map((d) => (
            <tr key={d.label}>
              <td>
                <span className="mono font-medium">{d.label}</span>
                {d.self && (
                  <Badge variant="outline" className="ml-2">
                    this device
                  </Badge>
                )}
              </td>
              <td className="mono">{d.meshIp?.split('/')[0] ?? '—'}</td>
              <td>
                {d.revoked ? (
                  <Badge variant="destructive">Revoked</Badge>
                ) : d.expired ? (
                  <Badge variant="destructive">Expired</Badge>
                ) : !d.pubkey ? (
                  <Badge variant="warning">No key yet</Badge>
                ) : d.self ? (
                  <Badge variant="success">Online</Badge>
                ) : (
                  <PathBadge connected={d.connected} path={d.path} />
                )}
              </td>
              <td className="mono text-xs">{d.fingerprint ?? '—'}</td>
              <td className="text-xs text-muted-foreground">
                {Number(d.expiry) ? new Date(Number(d.expiry) * 1000).toLocaleDateString() : '—'}
              </td>
              <td>
                <span className="flex items-center justify-end gap-2">
                  {!d.self && !d.revoked && d.pubkey && <PingButton label={d.label} />}
                  {!d.self && <RevokeButton device={d} />}
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </Card>
  )
}

export default function DevicesPage() {
  const { state } = useApp()
  return (
    <>
      <PageHeader
        title="Devices"
        description="Every name in this tailnet's registry, read from the local chain cache."
      />
      <NeedsTailnet>
        {state?.phase === 'member' && <Enroll />}
        <Section id="devices" title="Registered devices">
          <DeviceTable />
        </Section>
      </NeedsTailnet>
    </>
  )
}
