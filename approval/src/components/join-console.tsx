'use client'

import { useEffect, useRef, useState } from 'react'
import { JoinActivity, Members, PendingJoins, type JoinAction, type JoinRow } from '@bramble/join-ui'
import { readJson } from '../lib/console-api'
import { Button } from './ui/button'

export function JoinConsole({ token, view }: { token: string; view: 'pending' | 'members' }) {
  const [joins, setJoins] = useState<JoinRow[]>([])
  const [members, setMembers] = useState<JoinRow[]>([])
  const [queueError, setQueueError] = useState<string | null>(null)
  const [memberError, setMemberError] = useState<string | null>(null)
  const [queueLoaded, setQueueLoaded] = useState(false)
  const [membersLoaded, setMembersLoaded] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [refresh, setRefresh] = useState(0)
  const locked = useRef(false)
  useEffect(() => {
    if (!token) return
    const controller = new AbortController()
    let joinTimer: ReturnType<typeof setTimeout>
    let memberTimer: ReturnType<typeof setTimeout>
    async function poll(kind: 'joins' | 'members') {
      try {
        const response = await readJson<{ joins?: JoinRow[]; members?: JoinRow[] }>(`/api/${kind}`, {
          headers: { authorization: `Bearer ${token}` },
          signal: AbortSignal.any([controller.signal, AbortSignal.timeout(60_000)]),
        })
        if (controller.signal.aborted) return
        if (kind === 'joins') {
          setJoins(response.joins ?? [])
          setQueueError(null)
          setQueueLoaded(true)
        } else {
          setMembers(response.members ?? [])
          setMemberError(null)
          setMembersLoaded(true)
        }
      } catch (error) {
        if (!controller.signal.aborted) {
          const message = error instanceof Error ? error.message : 'The join service is unavailable.'
          if (kind === 'joins') setQueueError(message)
          else setMemberError(message)
        }
      } finally {
        if (!controller.signal.aborted) {
          if (kind === 'joins') joinTimer = setTimeout(() => void poll(kind), 1000)
          else memberTimer = setTimeout(() => void poll(kind), 10_000)
        }
      }
    }
    void poll('joins')
    if (view === 'members') void poll('members')
    return () => {
      controller.abort()
      clearTimeout(joinTimer)
      clearTimeout(memberTimer)
    }
  }, [token, refresh, view])

  async function decide(id: string, action: JoinAction) {
    if (locked.current) return
    locked.current = true
    setBusyId(id)
    setActionError(null)
    try {
      await readJson(`/api/joins/${encodeURIComponent(id)}/${action}`, {
        method: 'POST',
        headers: { authorization: `Bearer ${token}` },
      })
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'The device decision failed.')
    } finally {
      locked.current = false
      setBusyId(null)
      setRefresh((value) => value + 1)
    }
  }
  const disabled = !token || !!queueError || !!memberError
  const props = { disabled, busyId, onAction: (id: string, action: JoinAction) => void decide(id, action), Button }
  if (!token)
    return (
      <section aria-label="Device onboarding">
        <h2>Device onboarding</h2>
        <p className="empty">Unlock the console to view device joins and members.</p>
      </section>
    )
  const activity = [...joins, ...members.filter((member) => !joins.some((join) => join.id === member.id))]
  const alert = (queueError || memberError || actionError) && (
    <p role="alert" className="notice negative mb-4">
      {actionError ?? queueError ?? memberError}
    </p>
  )
  if (view === 'pending')
    return (
      <>
        {alert}
        {queueLoaded ? (
          <PendingJoins {...props} rows={joins} />
        ) : (
          !queueError && (
            <p role="status" className="empty">
              Loading device joins…
            </p>
          )
        )}
      </>
    )
  return (
    <>
      {alert}
      {membersLoaded ? (
        <Members {...props} rows={members} />
      ) : (
        !memberError && (
          <p role="status" className="empty">
            Loading ENS members…
          </p>
        )
      )}
      {activity.length > 0 && <JoinActivity rows={activity} />}
    </>
  )
}
