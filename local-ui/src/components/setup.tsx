'use client'

// What a device shows before it is a member: create a tailnet with your
// wallet, or join one by name and hand the admin a join code.
import { useEffect, useState, type FormEvent } from 'react'
import { formatEther, type Address } from 'viem'
import { Hourglass, Plus, ShieldOff, UserPlus } from 'lucide-react'
import { Button } from './ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { CopyButton, Problem, TxSteps, errorText } from './bits'
import { useApp } from './app-state'
import { api, shortAddress, type RelayInfo, type TailnetRef } from '../lib/api'
import * as tx from '../lib/chain/tx'
import { enrollSteps } from '../lib/enroll'
import { runSteps, useWallet, type TxStep } from '../lib/wallet'

const FAUCET = 'https://cloud.google.com/application/web3/faucet/ethereum/sepolia'
const FIRST_MESH_IP = '10.77.0.1/24'

function Field({
  id,
  label,
  hint,
  value,
  onChange,
  placeholder,
  invalid,
  suffix,
}: {
  id: string
  label: string
  hint?: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  invalid?: boolean
  suffix?: string
}) {
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <div className="flex items-center gap-2">
        <Input
          id={id}
          value={value}
          placeholder={placeholder}
          autoComplete="off"
          spellCheck={false}
          aria-invalid={invalid || undefined}
          onChange={(e) => onChange(e.target.value.toLowerCase().trim())}
          className="mono"
        />
        {suffix && <span className="mono shrink-0 text-xs text-muted-foreground">.{suffix}</span>}
      </div>
      {hint && <p className="m-0 text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}

function useRelays() {
  const [relays, setRelays] = useState<RelayInfo[]>()
  useEffect(() => {
    api
      .relays()
      .then(setRelays)
      .catch(() => setRelays([]))
  }, [])
  return relays
}

export function CreateTailnet() {
  const { state, refresh } = useApp()
  const w = useWallet()
  const relays = useRelays()
  const [name, setName] = useState('')
  const [device, setDevice] = useState('')
  const [relay, setRelay] = useState<string>()
  const [taken, setTaken] = useState<string>()
  const [steps, setSteps] = useState<TxStep[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const net = state?.network
  const usable = relays?.filter((r) => !r.error && r.price === '0') ?? []
  const chosenRelay = relay ?? usable[0]?.label ?? ''

  useEffect(() => {
    setTaken(undefined)
    if (!tx.LABEL_RE.test(name)) return
    const t = setTimeout(() => {
      api
        .lookup(name)
        .then((r) => setTaken(`${r.name} is taken (owner ${shortAddress(r.owner)}).`))
        .catch(() => setTaken(undefined))
    }, 400)
    return () => clearTimeout(t)
  }, [name])

  const valid = tx.LABEL_RE.test(name) && tx.LABEL_RE.test(device) && !taken
  const lowFunds = w.balance !== undefined && w.balance < 10n ** 16n

  async function create(e: FormEvent) {
    e.preventDefault()
    if (!net || !state) return
    setError(undefined)
    setBusy(true)
    try {
      const owner = (w.address ?? (await w.connect())) as Address | undefined
      if (!owner) throw new Error('Connect a wallet first.')
      let tailnet: tx.Tailnet | undefined
      const fullName = `${name}.${net.netSuffix}`
      const records = { pubkey: state.publicKey, meshIp: FIRST_MESH_IP, granters: [device] }
      const relayList = chosenRelay ? [chosenRelay] : undefined
      await runSteps(
        [
          {
            label: `Claim ${fullName}`,
            run: async () => {
              const r = await w.send(tx.claimTailnet(net.netRegistrar, name))
              const c = tx.claimedFromReceipt(r)
              tailnet = { name: fullName, registry: c.registry, resolver: c.resolver }
              // The daemon starts syncing the new registry while the next two writes land.
              await api.select(fullName, device, Number(c.block))
              return r
            },
          },
          { label: `Register ${device}.${fullName}`, run: () => w.send(tx.registerDevice(tailnet!, device, owner)) },
          {
            label: `Write ${device}'s key, address${relayList ? ' and the tailnet relay list' : ''}`,
            run: () => w.send(tx.writeDeviceRecords(tailnet!, device, records, relayList)),
          },
        ],
        setSteps
      )
      await refresh()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card className="gap-4">
      <CardHeader>
        <div>
          <CardTitle className="flex items-center gap-2">
            <Plus className="size-4" aria-hidden="true" /> Create a tailnet
          </CardTitle>
          <CardDescription className="mt-1.5">
            Your wallet claims a name under <span className="mono">{net?.netSuffix ?? 'net.bramble.eth'}</span> and
            becomes its admin. You pay the Sepolia gas yourself; nothing is sponsored.
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent>
        <form onSubmit={create} className="grid gap-4">
          <Field
            id="tailnet-name"
            label="Tailnet name"
            value={name}
            onChange={setName}
            placeholder="alice"
            suffix={net?.netSuffix}
            invalid={!!name && (!tx.LABEL_RE.test(name) || !!taken)}
            hint={taken ?? '3 to 32 lowercase letters, digits or hyphens. First come, first served.'}
          />
          <Field
            id="device-name-create"
            label="This device's name"
            value={device}
            onChange={setDevice}
            placeholder="laptop"
            invalid={!!device && !tx.LABEL_RE.test(device)}
            hint={`It becomes ${device || 'laptop'}.${name || 'alice'}.${net?.netSuffix ?? 'net.bramble.eth'} at ${FIRST_MESH_IP.split('/')[0]}.`}
          />
          <div className="grid gap-1.5">
            <Label htmlFor="relay">Relay</Label>
            <select id="relay" className="field" value={chosenRelay} onChange={(e) => setRelay(e.target.value)}>
              {usable.map((r) => (
                <option key={r.label} value={r.label}>
                  {r.label}
                  {r.region ? ` · ${r.region}` : ''}
                  {r.millis !== undefined ? ` · ${r.millis} ms` : ''}
                </option>
              ))}
              <option value="">Let each device pick from the directory</option>
            </select>
            <p className="m-0 text-xs text-muted-foreground">
              Relays only see sealed, encrypted packets. They introduce devices and carry traffic when a direct path
              fails.
            </p>
          </div>
          {lowFunds && (
            <p className="notice m-0 text-xs">
              This wallet holds {Number(formatEther(w.balance!)).toFixed(4)} SepoliaETH. Creating a tailnet takes about
              0.01 at typical Sepolia prices.{' '}
              <a href={FAUCET} target="_blank" rel="noreferrer">
                Get test ETH from a faucet
              </a>
              .
            </p>
          )}
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" disabled={!valid || busy || !w.available}>
              {busy ? 'Working…' : w.address ? 'Create tailnet' : 'Connect wallet and create'}
            </Button>
            <span className="text-xs text-muted-foreground">Three transactions: claim, register, records.</span>
          </div>
          {!w.available && (
            <p className="m-0 text-xs text-muted-foreground">You need a browser wallet to create a tailnet.</p>
          )}
        </form>
        <TxSteps steps={steps} />
        {error && (
          <div className="mt-4">
            <Problem>{error}</Problem>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

export function JoinTailnet() {
  const { refresh } = useApp()
  const [name, setName] = useState('')
  const [device, setDevice] = useState('')
  const [found, setFound] = useState<TailnetRef>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()

  async function find(e: FormEvent) {
    e.preventDefault()
    setError(undefined)
    setFound(undefined)
    setBusy(true)
    try {
      setFound(await api.lookup(name))
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }
  async function join() {
    if (!found) return
    setBusy(true)
    try {
      await api.select(found.name, device, found.start)
      await refresh()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Card className="gap-4">
      <CardHeader>
        <div>
          <CardTitle className="flex items-center gap-2">
            <UserPlus className="size-4" aria-hidden="true" /> Join a tailnet
          </CardTitle>
          <CardDescription className="mt-1.5">
            No wallet needed. This device gets a join code; the admin enrolls it from their own console.
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent>
        <form onSubmit={find} className="grid gap-4">
          <Field
            id="join-name"
            label="Tailnet name"
            value={name}
            onChange={setName}
            placeholder="alice or alice.net.bramble.eth"
          />
          <Field
            id="device-name-join"
            label="This device's name"
            value={device}
            onChange={setDevice}
            placeholder="server"
            invalid={!!device && !tx.LABEL_RE.test(device)}
            hint="Must not be taken in that tailnet."
          />
          {!found && (
            <div>
              <Button type="submit" variant="outline" disabled={!name || !tx.LABEL_RE.test(device) || busy}>
                {busy ? 'Looking up…' : 'Look up tailnet'}
              </Button>
            </div>
          )}
        </form>
        {found && (
          <div className="mt-4 rounded-lg border border-divider p-4 text-sm">
            <p className="m-0">
              Found <span className="mono font-medium">{found.name}</span>, owned by{' '}
              <span className="mono">{shortAddress(found.owner)}</span>.
            </p>
            <p className="mt-1 text-xs text-muted-foreground">Check the owner with the person who invited you.</p>
            <div className="mt-3 flex gap-2">
              <Button onClick={() => void join()} disabled={busy}>
                Join as {device}
              </Button>
              <Button variant="ghost" onClick={() => setFound(undefined)}>
                Cancel
              </Button>
            </div>
          </div>
        )}
        {error && (
          <div className="mt-4">
            <Problem>{error}</Problem>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

export function Syncing() {
  const { state } = useApp()
  const c = state?.chain
  return (
    <Card className="gap-3 px-5">
      <div className="flex items-center gap-3 text-base font-medium">
        <span className="pulse" aria-hidden="true" /> Reading {state?.tailnet?.name} from Sepolia
      </div>
      <p className="m-0 text-sm text-muted-foreground">
        The daemon replays the tailnet&apos;s registry events into a local cache. This takes a few seconds the first
        time.
        {c && ` Head ${c.head}, confirmed ${c.confirmed}.`}
      </p>
      {c?.error && <Problem>{c.error}</Problem>}
    </Card>
  )
}

export function Waiting() {
  const { state, devices, refresh } = useApp()
  const w = useWallet()
  const [steps, setSteps] = useState<TxStep[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  if (!state?.tailnet || !state.label) return null
  const tn = state.tailnet
  const label = state.label
  const self = devices?.find((d) => d.label === label)

  async function enrollSelf() {
    setBusy(true)
    setError(undefined)
    try {
      const owner = w.address ?? (await w.connect())
      if (!owner) throw new Error('Connect a wallet first.')
      const { meshIp } = await api.meshIp()
      await runSteps(
        enrollSteps(w.send, {
          tailnet: { name: tn.name, registry: tn.registry, resolver: tn.resolver },
          label,
          owner,
          pubkey: state!.publicKey,
          meshIp: self?.meshIp || meshIp,
          granters: [label],
          registered: !!self,
        }),
        setSteps
      )
      await refresh()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid gap-6">
      <Card className="gap-4 px-5">
        <div className="flex items-center gap-3 text-base font-medium">
          <Hourglass className="size-4 text-warning" aria-hidden="true" />
          Waiting for an admin of <span className="mono">{tn.name}</span>
        </div>
        <p className="m-0 text-sm text-muted-foreground">
          Send this join code to the admin. They paste it under Devices → Enroll and sign one or two transactions. This
          page moves on by itself once the records land on-chain.
        </p>
        <div className="rounded-lg border border-divider bg-secondary/40 p-3">
          <p className="eyebrow">Join code</p>
          <p className="mono mt-1 mb-0 break-all text-xs">{state.joinCode}</p>
          <div className="mt-3">
            <CopyButton value={state.joinCode ?? ''} label="Copy join code" />
          </div>
        </div>
        <div className="grid gap-1 text-sm sm:grid-cols-2">
          <div>
            <p className="eyebrow">Device</p>
            <p className="mono m-0">{label}</p>
          </div>
          <div>
            <p className="eyebrow">Key fingerprint</p>
            <p className="mono m-0">{state.fingerprint}</p>
            <p className="m-0 text-xs text-muted-foreground">
              Read it to the admin; their console shows the same digits.
            </p>
          </div>
        </div>
      </Card>
      {w.admin && (
        <Card className="gap-3 px-5">
          <p className="m-0 text-sm">
            Your wallet administers this tailnet. You can enroll this device yourself
            {self ? ' (its name is registered already; this writes its records).' : '.'}
          </p>
          <div>
            <Button onClick={() => void enrollSelf()} disabled={busy}>
              {busy ? 'Working…' : 'Enroll this device'}
            </Button>
          </div>
          <TxSteps steps={steps} />
          {error && <Problem>{error}</Problem>}
        </Card>
      )}
      <LeaveCard />
    </div>
  )
}

export function Revoked() {
  return (
    <div className="grid gap-6">
      <Card className="gap-2 px-5">
        <div className="flex items-center gap-3 text-base font-medium text-destructive">
          <ShieldOff className="size-4" aria-hidden="true" /> This device was revoked
        </div>
        <p className="m-0 text-sm text-muted-foreground">
          An admin marked it revoked on-chain, so peers no longer accept it. Ask the admin to restore it, or leave and
          join another tailnet.
        </p>
      </Card>
      <LeaveCard />
    </div>
  )
}

export function LeaveCard() {
  const { state, refresh } = useApp()
  const [confirm, setConfirm] = useState(false)
  if (!state?.tailnet) return null
  return (
    <Card className="gap-3 px-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="m-0 text-sm font-medium">Leave {state.tailnet.name}</p>
          <p className="m-0 text-xs text-muted-foreground">
            This device forgets the tailnet. Its on-chain records stay until an admin revokes them. The device key
            stays.
          </p>
        </div>
        {confirm ? (
          <div className="flex gap-2">
            <Button
              variant="destructive"
              onClick={() =>
                void api
                  .leave()
                  .then(refresh)
                  .finally(() => setConfirm(false))
              }
            >
              Leave
            </Button>
            <Button variant="ghost" onClick={() => setConfirm(false)}>
              Cancel
            </Button>
          </div>
        ) : (
          <Button variant="outline" onClick={() => setConfirm(true)}>
            Leave tailnet
          </Button>
        )}
      </div>
    </Card>
  )
}
