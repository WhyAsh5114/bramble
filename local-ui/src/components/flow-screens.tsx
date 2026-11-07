'use client'

import { useState, type FormEvent } from 'react'
import { Button } from '@approval/components/ui/button'
import { ApiFailure, joinNetwork, leaveNetwork, setupDevice } from '../lib/api'
import type { BrambleState } from '../lib/contract'
import { checkDeviceName, checkNetworkName } from '../lib/format'
import { Fact, Fingerprint, Notice, PageHeading } from './bits'
import { RequestAccessButton } from './connected-screen'

/** Runs one write call, then asks the page to re-read state so the next screen shows at once. */
function useAction(refresh: () => Promise<void>) {
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<ApiFailure | null>(null)
  async function run(action: () => Promise<unknown>) {
    if (busy) return
    setBusy(true)
    setFailure(null)
    try {
      await action()
      await refresh()
    } catch (error) {
      setFailure(error instanceof ApiFailure ? error : new ApiFailure('That did not work.', 'Try again.'))
    } finally {
      setBusy(false)
    }
  }
  return { busy, failure, run }
}

function DeviceIdentity({ state }: { state: BrambleState }) {
  return (
    <aside className="panel quiet" aria-label="This device">
      <p className="eyebrow">This device</p>
      <strong className="mono wrap device-name">{state.name || 'Not named yet'}</strong>
      <p className="eyebrow spaced">Fingerprint</p>
      <Fingerprint value={state.fingerprint} />
      <p className="muted small-print">
        The key behind this fingerprint stays on this device. Your admin can compare the fingerprint before approving.
      </p>
    </aside>
  )
}

export function SetupScreen({ state, refresh }: { state: BrambleState; refresh: () => Promise<void> }) {
  const [name, setName] = useState('')
  const [touched, setTouched] = useState(false)
  const { busy, failure, run } = useAction(refresh)
  const problem = touched ? checkDeviceName(name) : null
  function submit(event: FormEvent) {
    event.preventDefault()
    setTouched(true)
    if (checkDeviceName(name)) return
    void run(() => setupDevice(name.trim()))
  }
  return (
    <>
      <PageHeading eyebrow="Welcome" title="Name this device">
        Bramble gives every device a private address on your network. Pick a short name so other people recognise it.
      </PageHeading>
      <div className="split">
        <form className="panel" onSubmit={submit} noValidate>
          <label htmlFor="device-name" className="field-label">
            Device name
          </label>
          <input
            id="device-name"
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            placeholder="laptop-ana"
            value={name}
            aria-invalid={!!problem}
            aria-describedby="device-name-hint"
            onChange={(event) => setName(event.target.value.toLowerCase())}
            onBlur={() => setTouched(true)}
          />
          <p id="device-name-hint" className={problem ? 'negative field-hint' : 'muted field-hint'}>
            {problem ?? 'Lowercase letters, numbers and hyphens, like laptop-ana.'}
          </p>
          {failure && <Notice title={failure.message} nextStep={failure.nextStep} />}
          <div className="panel-actions">
            <Button className="primary" type="submit" disabled={busy}>
              {busy ? 'Creating identity…' : 'Continue'}
            </Button>
          </div>
        </form>
        <DeviceIdentity state={state} />
      </div>
    </>
  )
}

export function JoinScreen({ state, refresh }: { state: BrambleState; refresh: () => Promise<void> }) {
  const [tailnet, setTailnet] = useState('')
  const [touched, setTouched] = useState(false)
  const { busy, failure, run } = useAction(refresh)
  const problem = touched ? checkNetworkName(tailnet) : null
  function submit(event: FormEvent) {
    event.preventDefault()
    setTouched(true)
    if (checkNetworkName(tailnet)) return
    void run(() => joinNetwork(tailnet.trim().toLowerCase()))
  }
  return (
    <>
      <PageHeading eyebrow="Join a network" title="Which network do you want to join?">
        Ask your network admin for the network name. The admin approves each device before it can connect.
      </PageHeading>
      <div className="split">
        <form className="panel" onSubmit={submit} noValidate>
          <label htmlFor="network-name" className="field-label">
            Network name
          </label>
          <input
            id="network-name"
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            placeholder="acme.bramble.eth"
            value={tailnet}
            aria-invalid={!!problem}
            aria-describedby="network-name-hint"
            onChange={(event) => setTailnet(event.target.value)}
            onBlur={() => setTouched(true)}
          />
          <p id="network-name-hint" className={problem ? 'negative field-hint' : 'muted field-hint'}>
            {problem ?? 'Looks like a web address, for example acme.bramble.eth.'}
          </p>
          {failure && <Notice title={failure.message} nextStep={failure.nextStep} />}
          <div className="panel-actions">
            <Button className="primary" type="submit" disabled={busy}>
              {busy ? 'Sending request…' : 'Request to join'}
            </Button>
          </div>
        </form>
        <DeviceIdentity state={state} />
      </div>
    </>
  )
}

export function PendingScreen({ state, refresh }: { state: BrambleState; refresh: () => Promise<void> }) {
  const { busy, failure, run } = useAction(refresh)
  return (
    <>
      <PageHeading eyebrow="Request sent" title="Waiting for approval">
        Your admin needs to approve this device. This page updates by itself, so you can leave it open.
      </PageHeading>
      <div className="split">
        <section className="panel" aria-label="Request status">
          <p className="live" role="status">
            <span className="pulse" aria-hidden="true" />
            Waiting for the admin of <strong className="mono wrap">{state.tailnet}</strong>
          </p>
          <div className="facts">
            <Fact label="Device name">
              <span className="mono">{state.name}</span>
            </Fact>
            {state.joinId && (
              <Fact label="Request">
                <span className="mono">{state.joinId}</span>
              </Fact>
            )}
          </div>
          <p className="muted small-print">
            Tell your admin this device is waiting, and read them the fingerprint so they know the request is yours.
          </p>
          {failure && <Notice title={failure.message} nextStep={failure.nextStep} />}
          <div className="panel-actions">
            <Button variant="outline" disabled={busy} onClick={() => void run(leaveNetwork)}>
              Cancel request
            </Button>
          </div>
        </section>
        <DeviceIdentity state={state} />
      </div>
    </>
  )
}

export function ApprovedScreen({ state }: { state: BrambleState }) {
  return (
    <>
      <PageHeading eyebrow="Approved" title="You are in. Connecting…">
        <span>
          <strong className="mono">{state.tailnet}</strong> approved this device. Bramble is setting up your private
          address.
        </span>
      </PageHeading>
      <section className="panel" aria-label="Connection progress">
        <p className="live positive" role="status">
          <span className="pulse" aria-hidden="true" />
          {state.meshIp ? (
            <>
              Your address is <strong className="mono">{state.meshIp}</strong>. Finding other devices…
            </>
          ) : (
            'Getting your private address…'
          )}
        </p>
      </section>
      {state.services.map((link) => (
        <section key={`${link.peer}/${link.service}`} className="panel" aria-label={`Access to ${link.service}`}>
          <p>
            Ask the owner for access to <strong className="mono">{link.service}</strong> on{' '}
            <span className="mono">{link.peer}</span>.
          </p>
          <RequestAccessButton peer={link.peer} service={link.service} />
        </section>
      ))}
    </>
  )
}

export function DeniedScreen({ state, refresh }: { state: BrambleState; refresh: () => Promise<void> }) {
  const { busy, failure, run } = useAction(refresh)
  return (
    <>
      <PageHeading eyebrow="Not approved" title="This device cannot join yet" />
      <section className="panel declined" aria-label="Request result">
        <p>
          <strong className="mono wrap">{state.tailnet}</strong> did not approve{' '}
          <span className="mono">{state.name}</span>, or removed it later.
        </p>
        <p className="muted">
          Ask the admin to check the request. If you used the wrong network name, start over with the right one.
        </p>
        {failure && <Notice title={failure.message} nextStep={failure.nextStep} />}
        <div className="panel-actions">
          <Button className="primary" disabled={busy} onClick={() => void run(leaveNetwork)}>
            Start over
          </Button>
        </div>
      </section>
    </>
  )
}

export function ErrorScreen({ state, refresh }: { state: BrambleState; refresh: () => Promise<void> }) {
  const { busy, failure, run } = useAction(refresh)
  return (
    <>
      <PageHeading eyebrow="Needs attention" title="Bramble hit a problem" />
      <section className="panel" aria-label="Problem details">
        <Notice
          title={state.error?.message ?? 'Something went wrong.'}
          nextStep={state.error?.nextStep || 'Try again. If it keeps happening, run `bramble status` in a terminal.'}
        />
        {failure && <Notice title={failure.message} nextStep={failure.nextStep} />}
        <div className="panel-actions">
          <Button variant="outline" disabled={busy} onClick={() => void run(() => Promise.resolve())}>
            Check again
          </Button>
          {/* A first join can fail before it has a network or join id, so reset is always offered. */}
          <Button variant="outline" disabled={busy} onClick={() => void run(leaveNetwork)}>
            {state.tailnet || state.joinId ? 'Leave network' : 'Start over'}
          </Button>
        </div>
      </section>
    </>
  )
}

export function UnreachableScreen({ failure }: { failure: ApiFailure }) {
  return (
    <>
      <PageHeading eyebrow="Offline" title="Bramble is not running" />
      <section className="panel" aria-label="Problem details">
        <Notice title={failure.message} nextStep={failure.nextStep} />
        <p className="muted small-print">This page keeps trying and recovers on its own once Bramble answers.</p>
      </section>
    </>
  )
}
