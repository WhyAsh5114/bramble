'use client'

import { useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { ApiFailure, decideJoin } from '../lib/api'
import type { JoinRecord } from '../lib/contract'
import { shortKey } from '../lib/format'
import { Notice, PageHeading } from './bits'

const statusText = {
  pending: 'Awaiting decision',
  approved: 'Approved',
  denied: 'Denied',
  revoked: 'Revoked',
}

function when(value?: string) {
  const millis = value ? Date.parse(value) : NaN
  return Number.isFinite(millis) ? new Date(millis).toLocaleTimeString() : null
}

function JoinCard({
  join,
  decide,
  disabled,
}: {
  join: JoinRecord
  decide: (join: JoinRecord, action: 'approve' | 'deny' | 'revoke') => Promise<void>
  disabled: boolean
}) {
  const [busy, setBusy] = useState<string | null>(null)
  const [confirmRevoke, setConfirmRevoke] = useState(false)
  const received = when(join.createdAt)
  async function run(action: 'approve' | 'deny' | 'revoke') {
    setBusy(action)
    await decide(join, action)
    setBusy(null)
    setConfirmRevoke(false)
  }
  const off = disabled || busy !== null
  return (
    <article className="request-card" aria-label={`Join request from ${join.label}`}>
      <header className="card-header">
        <div>
          <p className="eyebrow">{join.status === 'pending' ? 'Join request' : 'Device'}</p>
          <h3 className="mono wrap">{join.label}</h3>
        </div>
        <span className={`status ${join.status === 'approved' ? 'positive' : ''}`}>
          <span className="status-dot" />
          {statusText[join.status]}
        </span>
      </header>
      <div className="request-details">
        <div>
          <span className="eyebrow">Network</span>
          <strong className="mono wrap">{join.tailnet || '—'}</strong>
        </div>
        <div>
          <span className="eyebrow">Device key</span>
          <strong className="mono">{join.pubkey ? shortKey(join.pubkey) : '—'}</strong>
        </div>
        <div>
          <span className="eyebrow">{join.meshIp ? 'Address' : 'Received'}</span>
          <strong className="mono">{join.meshIp ?? received ?? '—'}</strong>
        </div>
      </div>
      <footer className="card-footer">
        <span className="muted">
          {join.status === 'pending' ? 'Check the device key with the person who asked.' : `Id ${join.id}`}
        </span>
        <div className="actions">
          {join.status === 'pending' && (
            <>
              <Button variant="outline" disabled={off} onClick={() => void run('deny')}>
                {busy === 'deny' ? 'Denying…' : 'Deny'}
              </Button>
              <Button className="primary" disabled={off} onClick={() => void run('approve')}>
                {busy === 'approve' ? 'Approving…' : 'Approve'}
              </Button>
            </>
          )}
          {join.status === 'approved' &&
            (confirmRevoke ? (
              <>
                <Button variant="outline" disabled={off} onClick={() => setConfirmRevoke(false)}>
                  Keep
                </Button>
                <Button variant="destructive" className="danger" disabled={off} onClick={() => void run('revoke')}>
                  {busy === 'revoke' ? 'Revoking…' : `Revoke ${join.label}`}
                </Button>
              </>
            ) : (
              <Button variant="destructive" className="danger" disabled={off} onClick={() => setConfirmRevoke(true)}>
                Revoke access
              </Button>
            ))}
        </div>
      </footer>
    </article>
  )
}

export function AdminView({
  joins,
  members,
  error,
  refresh,
}: {
  joins: JoinRecord[] | null
  members: JoinRecord[] | null
  error: ApiFailure | null
  refresh: () => Promise<void>
}) {
  const [actionError, setActionError] = useState<ApiFailure | null>(null)
  const locked = useRef(false)
  const pending = joins?.filter((join) => join.status === 'pending') ?? []
  const closed = joins?.filter((join) => join.status === 'denied' || join.status === 'revoked') ?? []
  // Members come from their own endpoint; fall back to approved joins if it is empty.
  const active = members && members.length > 0 ? members : (joins?.filter((join) => join.status === 'approved') ?? [])

  async function decide(join: JoinRecord, action: 'approve' | 'deny' | 'revoke') {
    if (locked.current) return
    locked.current = true
    setActionError(null)
    try {
      await decideJoin(join.id, action)
      await refresh()
    } catch (failure) {
      setActionError(failure instanceof ApiFailure ? failure : new ApiFailure('The decision failed.', 'Try again.'))
    } finally {
      locked.current = false
    }
  }

  const loaded = joins !== null
  return (
    <>
      <PageHeading eyebrow="Admin" title="Who can join your network">
        Approve a device only when you have checked its name and key with the person who asked.
      </PageHeading>
      {error && <Notice title={error.message} nextStep={error.nextStep} />}
      {actionError && <Notice title={actionError.message} nextStep={actionError.nextStep} />}
      <div className="summary" aria-label="Admin summary">
        <div>
          <span className="eyebrow">Awaiting decision</span>
          <strong>{loaded ? pending.length : '—'}</strong>
        </div>
        <div>
          <span className="eyebrow">Devices on the network</span>
          <strong className="positive">{loaded ? active.length : '—'}</strong>
        </div>
        <div>
          <span className="eyebrow">Closed requests</span>
          <strong>{loaded ? closed.length : '—'}</strong>
        </div>
      </div>
      {!loaded && !error && (
        <p role="status" className="empty">
          Loading join requests…
        </p>
      )}
      <section aria-labelledby="join-title">
        <div className="section-heading">
          <h2 id="join-title">Join requests</h2>
          <span className="muted">Each one needs your decision</span>
        </div>
        {loaded && pending.length === 0 && (
          <p className="empty">No requests waiting. New devices appear here as soon as they ask to join.</p>
        )}
        {pending.map((join) => (
          <JoinCard key={join.id} join={join} decide={decide} disabled={!!error} />
        ))}
      </section>
      <section aria-labelledby="members-title">
        <div className="section-heading">
          <h2 id="members-title">Devices on the network</h2>
          <span className="muted">Revoking removes a device from the network</span>
        </div>
        {loaded && active.length === 0 && <p className="empty">No approved devices yet.</p>}
        {active.map((join) => (
          <JoinCard key={join.id} join={join} decide={decide} disabled={!!error} />
        ))}
      </section>
      {closed.length > 0 && (
        <section aria-labelledby="closed-title">
          <div className="section-heading">
            <h2 id="closed-title">Closed requests</h2>
          </div>
          {closed.map((join) => (
            <JoinCard key={join.id} join={join} decide={decide} disabled={!!error} />
          ))}
        </section>
      )}
    </>
  )
}
