'use client'

import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { ExternalLink, Trash2 } from 'lucide-react'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../../components/ui/card'
import { Input } from '../../components/ui/input'
import { Label } from '../../components/ui/label'
import { useApp } from '../../components/app-state'
import { Empty, PageHeader, Problem, Section, errorText } from '../../components/bits'
import { api, type Forward, type PeerService } from '../../lib/api'

const NAME_RE = /^[a-z0-9][a-z0-9-]{0,31}$/

function OwnServices() {
  const { state, refresh } = useApp()
  const [name, setName] = useState('')
  const [port, setPort] = useState('')
  const [open, setOpen] = useState(false)
  const [error, setError] = useState<string>()
  if (!state) return null
  const custom = state.services.filter((s) => !s.builtIn)

  async function save(services: typeof custom, ping: boolean) {
    setError(undefined)
    try {
      await api.setServices(
        services.map(({ name, port, open }) => ({ name, port, open })),
        ping
      )
      await refresh()
      return true
    } catch (e) {
      setError(errorText(e))
      return false
    }
  }
  async function add(e: FormEvent) {
    e.preventDefault()
    if (
      await save([...custom.filter((s) => s.name !== name), { name, port: Number(port), open }], state!.settings.ping)
    ) {
      setName('')
      setPort('')
      setOpen(false)
    }
  }

  return (
    <Section id="mine" title="Shared by this device" hint="Peers reach these through the tunnel">
      <Card className="gap-0 overflow-x-auto py-0">
        <table className="data-table">
          <thead>
            <tr>
              <th>Service</th>
              <th>Local port</th>
              <th>Who may connect</th>
              <th className="text-right">Action</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td className="mono">ping</td>
              <td className="mono">{state.services.find((s) => s.builtIn)?.port ?? '—'}</td>
              <td>
                {state.settings.ping ? (
                  <Badge variant="success">Every member</Badge>
                ) : (
                  <Badge variant="outline">Off</Badge>
                )}
              </td>
              <td className="text-right">
                <Button variant="outline" size="sm" onClick={() => void save(custom, !state.settings.ping)}>
                  {state.settings.ping ? 'Turn off' : 'Turn on'}
                </Button>
              </td>
            </tr>
            {custom.map((s) => (
              <tr key={s.name}>
                <td className="mono">{s.name}</td>
                <td className="mono">127.0.0.1:{s.port}</td>
                <td>
                  <button
                    type="button"
                    className="cursor-pointer border-0 bg-transparent p-0"
                    title="Click to change"
                    onClick={() =>
                      void save(
                        custom.map((x) => (x.name === s.name ? { ...x, open: !x.open } : x)),
                        state.settings.ping
                      )
                    }
                  >
                    {s.open ? (
                      <Badge variant="success">Every member</Badge>
                    ) : (
                      <Badge variant="warning">Granted devices</Badge>
                    )}
                  </button>
                </td>
                <td className="text-right">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Stop sharing ${s.name}`}
                    onClick={() =>
                      void save(
                        custom.filter((x) => x.name !== s.name),
                        state.settings.ping
                      )
                    }
                  >
                    <Trash2 />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
      <form onSubmit={add} className="mt-4 flex flex-wrap items-end gap-3">
        <div className="grid gap-1.5">
          <Label htmlFor="svc-name">Name</Label>
          <Input
            id="svc-name"
            className="mono w-40"
            value={name}
            onChange={(e) => setName(e.target.value.toLowerCase().trim())}
            placeholder="web"
          />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="svc-port">Local port</Label>
          <Input
            id="svc-port"
            className="mono w-28"
            inputMode="numeric"
            value={port}
            onChange={(e) => setPort(e.target.value.replace(/\D/g, ''))}
            placeholder="8080"
          />
        </div>
        <label className="flex h-9 items-center gap-2 text-sm">
          <input type="checkbox" checked={open} onChange={(e) => setOpen(e.target.checked)} />
          Every member may connect
        </label>
        <Button
          type="submit"
          disabled={!NAME_RE.test(name) || name === 'ping' || !port || Number(port) < 1 || Number(port) > 65535}
        >
          Share
        </Button>
      </form>
      <p className="mt-2 text-xs text-muted-foreground">
        Without &quot;every member&quot;, only devices you grant on the Access page can connect.
      </p>
      {error && (
        <div className="mt-3">
          <Problem>{error}</Problem>
        </div>
      )}
    </Section>
  )
}

function PeerServices() {
  const { devices } = useApp()
  const [lists, setLists] = useState<Record<string, PeerService[] | string>>({})
  const [forwards, setForwards] = useState<Forward[]>([])
  const [error, setError] = useState<string>()
  const peers = devices?.filter((d) => !d.self && !d.revoked && d.pubkey) ?? []
  const peerKey = peers.map((p) => `${p.label}:${p.connected}`).join(',')

  const loadForwards = useCallback(
    () =>
      api
        .forwards()
        .then(setForwards)
        .catch(() => {}),
    []
  )
  useEffect(() => {
    void loadForwards()
  }, [loadForwards])
  useEffect(() => {
    for (const p of peerKey.split(',').filter(Boolean)) {
      const [label, connected] = p.split(':')
      if (connected !== 'true') continue
      api
        .peerServices(label)
        .then((s) => setLists((l) => ({ ...l, [label]: s })))
        .catch((e) => setLists((l) => ({ ...l, [label]: errorText(e) })))
    }
  }, [peerKey])

  async function openForward(peer: string, service: string) {
    setError(undefined)
    try {
      await api.openForward(peer, service)
      await loadForwards()
    } catch (e) {
      setError(errorText(e))
    }
  }

  return (
    <Section id="peers" title="On other devices" hint="Only services you may use are listed">
      {error && <Problem>{error}</Problem>}
      {peers.length === 0 ? (
        <Empty>No other devices yet.</Empty>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {peers.map((p) => {
            const list = lists[p.label]
            return (
              <Card key={p.label} className="gap-3">
                <CardHeader>
                  <div>
                    <CardTitle className="mono text-sm">{p.label}</CardTitle>
                    <CardDescription className="mono mt-1 text-xs">{p.meshIp?.split('/')[0]}</CardDescription>
                  </div>
                  {!p.connected && <Badge variant="outline">Not connected</Badge>}
                </CardHeader>
                <CardContent>
                  {!p.connected ? (
                    <p className="m-0 text-xs text-muted-foreground">Services appear once the tunnel is up.</p>
                  ) : typeof list === 'string' ? (
                    <p className="m-0 text-xs text-destructive">{list}</p>
                  ) : !list ? (
                    <p className="m-0 text-xs text-muted-foreground">Asking…</p>
                  ) : list.length === 0 ? (
                    <p className="m-0 text-xs text-muted-foreground">Nothing shared with you.</p>
                  ) : (
                    <ul className="m-0 grid list-none gap-2 p-0">
                      {list.map((s) => {
                        const f = forwards.find((x) => x.peer === p.label && x.service === s.name)
                        return (
                          <li key={s.name} className="flex items-center gap-2 text-sm">
                            <span className="mono flex-1">{s.name}</span>
                            <Badge variant={s.access === 'tailnet' ? 'outline' : 'success'}>
                              {s.access === 'tailnet' ? 'open' : 'granted'}
                            </Badge>
                            {f ? (
                              <>
                                <a
                                  className="mono inline-flex items-center gap-1 text-xs"
                                  href={`http://127.0.0.1:${f.port}/`}
                                  target="_blank"
                                  rel="noreferrer"
                                >
                                  127.0.0.1:{f.port} <ExternalLink className="size-3" aria-hidden="true" />
                                </a>
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  onClick={() => void api.closeForward(f.port).then(setForwards)}
                                >
                                  Close
                                </Button>
                              </>
                            ) : (
                              <Button variant="outline" size="sm" onClick={() => void openForward(p.label, s.name)}>
                                Open
                              </Button>
                            )}
                          </li>
                        )
                      })}
                    </ul>
                  )}
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}
      <p className="mt-3 text-xs text-muted-foreground">
        Open maps the service to a port on this machine&apos;s loopback address. Anything on this machine can use that
        port while it is open.
      </p>
    </Section>
  )
}

export default function ServicesPage() {
  const { state } = useApp()
  return (
    <>
      <PageHeader title="Services" description="What this device shares with the tailnet, and what it can reach." />
      <OwnServices />
      {state?.phase === 'member' && <PeerServices />}
    </>
  )
}
