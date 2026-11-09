'use client'

import { useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { ApiFailure, leaveNetwork, requestAccess, testConnection } from '../lib/api'
import type { BrambleState, Peer } from '../lib/contract'
import { describeTest, pathCopy, relativeTime } from '../lib/format'
import { CopyValue, Fact, Fingerprint, Notice, PageHeading } from './bits'

interface Outcome {
  target: string
  ok: boolean
  text: string
}

function PathBadge({ path }: { path: Peer['path'] }) {
  const { label, hint } = pathCopy[path]
  return (
    <span className={`badge path-${path}`} title={hint}>
      <span className="status-dot" aria-hidden="true" />
      {label}
    </span>
  )
}

/** Asks the owner for one service. The owner still has to approve it. */
export function RequestAccessButton({ peer, service }: { peer: string; service: string }) {
  const [state, setState] = useState<'idle' | 'busy' | 'sent' | 'failed'>('idle')
  async function send() {
    setState('busy')
    try {
      await requestAccess(peer, service)
      setState('sent')
    } catch {
      setState('failed')
    }
  }
  return (
    <Button disabled={state === 'busy' || state === 'sent'} onClick={() => void send()}>
      {state === 'busy'
        ? 'Requesting…'
        : state === 'sent'
          ? 'Requested'
          : state === 'failed'
            ? 'Try again'
            : 'Request access'}
    </Button>
  )
}

export function ConnectedScreen({ state, refresh }: { state: BrambleState; refresh: () => Promise<void> }) {
  const [testing, setTesting] = useState<string | null>(null)
  const [outcome, setOutcome] = useState<Outcome | null>(null)
  const [confirmLeave, setConfirmLeave] = useState(false)
  const [leaving, setLeaving] = useState(false)
  const [leaveFailure, setLeaveFailure] = useState<ApiFailure | null>(null)
  const locked = useRef(false)

  async function runTest(peer?: string) {
    if (locked.current) return
    locked.current = true
    const target = peer ?? 'the network'
    setTesting(peer ?? '*')
    setOutcome(null)
    try {
      const result = await testConnection(peer)
      setOutcome({ target, ...describeTest(result, target) })
    } catch (failure) {
      const reason = failure instanceof ApiFailure ? `${failure.message} ${failure.nextStep}` : 'The test did not run.'
      setOutcome({ target, ok: false, text: reason })
    } finally {
      setTesting(null)
      locked.current = false
    }
  }

  async function leave() {
    setLeaving(true)
    setLeaveFailure(null)
    try {
      await leaveNetwork()
      await refresh()
    } catch (failure) {
      setLeaveFailure(failure instanceof ApiFailure ? failure : new ApiFailure('Could not leave.', 'Try again.'))
      setLeaving(false)
    }
  }

  const direct = state.peers.filter((peer) => peer.path === 'direct').length
  return (
    <>
      <PageHeading eyebrow="Connected" title="You are on the network">
        <span>
          This device is part of <strong className="mono">{state.tailnet}</strong>. Other devices reach it at the
          address below.
        </span>
      </PageHeading>
      <div className="summary" aria-label="Connection summary">
        <div>
          <span className="eyebrow">Your address</span>
          <strong className="mono address">
            {state.meshIp ? <CopyValue value={state.meshIp} label="address" /> : '—'}
          </strong>
        </div>
        <div>
          <span className="eyebrow">Other devices</span>
          <strong>{state.peers.length}</strong>
        </div>
        <div>
          <span className="eyebrow">Direct paths</span>
          <strong className={direct > 0 ? 'positive' : ''}>{direct}</strong>
        </div>
      </div>

      <section aria-labelledby="test-title" className="panel test-panel">
        <div>
          <h2 id="test-title">Check that it works</h2>
          <p className="muted">Sends a small request through the network and reports how long it took.</p>
        </div>
        <Button className="primary" disabled={testing !== null} onClick={() => void runTest()}>
          {testing === '*' ? 'Testing…' : 'Test connection'}
        </Button>
      </section>
      <div aria-live="polite" className="outcome-slot">
        {outcome && (
          <p role="status" className={`outcome ${outcome.ok ? 'ok' : 'bad'}`}>
            <span className="status-dot" aria-hidden="true" />
            <span>
              {outcome.text}
              {!outcome.ok && <span className="muted"> Check that the other device is online, then test again.</span>}
            </span>
          </p>
        )}
      </div>

      {state.services.length > 0 && (
        <section aria-labelledby="services-title">
          <div className="section-heading">
            <h2 id="services-title">Private services</h2>
            <span className="muted">The other device decides each visit</span>
          </div>
          <ul className="service-list">
            {state.services.map((link) => (
              <li key={`${link.peer}/${link.service}`} className="panel service">
                <div className="peer-id">
                  <strong className="mono wrap">{link.service}</strong>
                  <span className="muted mono">on {link.peer}</span>
                </div>
                <RequestAccessButton peer={link.peer} service={link.service} />
                <a className="open-link" href={link.url} target="_blank" rel="noopener noreferrer">
                  Open {link.service}
                </a>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section aria-labelledby="peers-title">
        <div className="section-heading">
          <h2 id="peers-title">Devices on this network</h2>
          <span className="muted">Updates every second</span>
        </div>
        {state.peers.length === 0 ? (
          <p className="empty">
            No other devices yet. When someone else joins <span className="mono">{state.tailnet}</span> and is approved,
            they show up here.
          </p>
        ) : (
          <ul className="peer-list">
            {state.peers.map((peer) => (
              <li key={peer.label} className="peer" aria-label={`Device ${peer.label}`}>
                <div className="peer-id">
                  <strong className="mono wrap">{peer.label}</strong>
                  <span className="muted mono">{peer.meshIp}</span>
                </div>
                <PathBadge path={peer.path} />
                <div className="peer-meta contact">
                  <span className="muted">Last contact</span>
                  <span>{relativeTime(peer.lastHandshake)}</span>
                </div>
                <div className="peer-meta rtt">
                  <span className="muted">Round trip</span>
                  <span>{peer.rttMs === undefined ? '—' : `${Math.round(peer.rttMs)} ms`}</span>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={`Test connection to ${peer.label}`}
                  disabled={testing !== null}
                  onClick={() => void runTest(peer.label)}
                >
                  {testing === peer.label ? 'Testing…' : 'Test'}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section aria-labelledby="device-title" className="device-section">
        <div className="section-heading">
          <h2 id="device-title">This device</h2>
        </div>
        <div className="facts panel quiet">
          <Fact label="Name">
            <span className="mono">{state.name}</span>
          </Fact>
          <Fact label="Fingerprint">
            <Fingerprint value={state.fingerprint} />
          </Fact>
        </div>
        {leaveFailure && <Notice title={leaveFailure.message} nextStep={leaveFailure.nextStep} />}
        <div className="leave">
          {confirmLeave ? (
            <>
              <span>Leave {state.tailnet}? This device loses its address and will need approval again to rejoin.</span>
              <span className="actions">
                <Button variant="outline" disabled={leaving} onClick={() => setConfirmLeave(false)}>
                  Stay
                </Button>
                <Button variant="destructive" className="danger" disabled={leaving} onClick={() => void leave()}>
                  {leaving ? 'Leaving…' : 'Leave network'}
                </Button>
              </span>
            </>
          ) : (
            <Button variant="outline" onClick={() => setConfirmLeave(true)}>
              Leave network
            </Button>
          )}
        </div>
      </section>
    </>
  )
}
