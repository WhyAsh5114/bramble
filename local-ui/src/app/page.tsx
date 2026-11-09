'use client'

import Link from 'next/link'
import { Card } from '../components/ui/card'
import { useApp } from '../components/app-state'
import { CopyButton, Empty, PageHeader, PathBadge, Section, Stats } from '../components/bits'
import { PingButton } from '../components/ping'
import { CreateTailnet, JoinTailnet, Revoked, Syncing, Waiting } from '../components/setup'
import { ago } from '../lib/api'

const INSTALL = 'curl -fsSL https://get-bramble.whyash5114.com/install.sh | sh'

function Member() {
  const { state, devices } = useApp()
  if (!state?.tailnet) return null
  const peers = devices?.filter((d) => !d.self && !d.revoked && d.pubkey) ?? []
  const online = peers.filter((d) => d.connected)
  return (
    <>
      <Stats
        items={[
          ['Peers online', devices ? `${online.length} / ${peers.length}` : '—'],
          [
            'Mesh address',
            <span key="ip" className="mono text-lg">
              {state.node?.address ?? '—'}
            </span>,
          ],
          [
            'Public endpoint',
            <span key="ep" className="mono text-lg">
              {state.node?.endpoint ?? (state.node?.stun ? `STUN ${state.node.stun}` : '—')}
            </span>,
          ],
          ['Chain synced', state.chain ? ago(state.chain.lastSync) : '—'],
        ]}
      />
      <div className="grid items-start gap-8 lg:grid-cols-[minmax(0,1fr)_340px]">
        <Section id="peers" title="Peers" hint={<Link href="/devices">All devices</Link>}>
          {peers.length === 0 ? (
            <Empty>No other devices yet. Invite one with the steps on the right.</Empty>
          ) : (
            <Card className="gap-0 py-0">
              <ul className="m-0 list-none p-0">
                {peers.map((d) => (
                  <li
                    key={d.label}
                    className="flex flex-wrap items-center gap-3 border-b border-divider px-5 py-3 last:border-b-0"
                  >
                    <span className="min-w-0 flex-1">
                      <span className="mono block truncate text-sm font-medium">{d.label}</span>
                      <span className="mono block text-xs text-muted-foreground">{d.meshIp?.split('/')[0]}</span>
                    </span>
                    <PathBadge connected={d.connected} path={d.path} />
                    <PingButton label={d.label} />
                  </li>
                ))}
              </ul>
            </Card>
          )}
        </Section>
        <Section id="invite" title="Add a device">
          <Card className="gap-3 px-5 text-sm">
            <ol className="m-0 grid gap-3 pl-4">
              <li>
                On the other machine, install bramble:
                <code className="mono mt-1 block break-all rounded-md bg-secondary px-2 py-1.5 text-xs">{INSTALL}</code>
                <div className="mt-2">
                  <CopyButton value={INSTALL} label="Copy command" />
                </div>
              </li>
              <li>
                In its console, choose <strong>Join a tailnet</strong> and enter{' '}
                <span className="mono">{state.tailnet.name}</span>.
              </li>
              <li>
                Paste the join code it shows into <Link href="/devices">Devices → Enroll</Link> and sign with the admin
                wallet.
              </li>
            </ol>
          </Card>
        </Section>
      </div>
    </>
  )
}

export default function Overview() {
  const { state } = useApp()
  if (!state) return <PageHeader title="Overview" description="Connecting to the bramble daemon on this machine…" />
  switch (state.phase) {
    case 'setup':
      return (
        <>
          <PageHeader
            title="Get started"
            description="A tailnet is a private network whose membership lives in an ENS registry you own on Sepolia."
          />
          {state.problem && <p className="notice mb-4 text-sm">{state.problem}</p>}
          <div className="grid items-start gap-6 lg:grid-cols-2">
            <CreateTailnet />
            <JoinTailnet />
          </div>
        </>
      )
    case 'syncing':
      return (
        <>
          <PageHeader title="Overview" />
          <Syncing />
        </>
      )
    case 'waiting':
      return (
        <>
          <PageHeader title="Almost there" description="This device has a name but is not a member yet." />
          <Waiting />
        </>
      )
    case 'revoked':
      return (
        <>
          <PageHeader title="Overview" />
          <Revoked />
        </>
      )
    default:
      return (
        <>
          <PageHeader
            title={state.label ?? 'Overview'}
            description={`Member of ${state.tailnet?.name}. Key fingerprint ${state.fingerprint}.`}
          />
          {state.problem && <p className="notice mb-4 text-sm">{state.problem}</p>}
          <Member />
        </>
      )
  }
}
