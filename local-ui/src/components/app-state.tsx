'use client'

// One poll loop for the whole page: daemon state every 2 s, the tailnet's
// devices on the same tick once a tailnet is chosen. Pages read from here.
import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api, type Device, type State } from '../lib/api'
import { WalletProvider } from '../lib/wallet'

interface AppState {
  state?: State
  devices?: Device[]
  error?: string
  updatedAt?: Date
  refresh: () => Promise<void>
}

const Ctx = createContext<AppState | null>(null)

export function useApp() {
  const c = useContext(Ctx)
  if (!c) throw new Error('useApp outside AppStateProvider')
  return c
}

/** The state of a page that needs one, or undefined while the first poll runs. */
export function useTailnet() {
  const { state } = useApp()
  return state?.tailnet ? { ...state.tailnet, label: state.label ?? '' } : undefined
}

export function AppStateProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<State>()
  const [devices, setDevices] = useState<Device[]>()
  const [error, setError] = useState<string>()
  const [updatedAt, setUpdatedAt] = useState<Date>()

  const refresh = useCallback(async () => {
    try {
      const s = await api.state()
      setState(s)
      setError(undefined)
      setUpdatedAt(new Date())
      if (s.tailnet) setDevices(await api.devices().catch(() => undefined))
      else setDevices(undefined)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }, [])

  useEffect(() => {
    let stop = false
    const tick = async () => {
      if (stop) return
      await refresh()
      if (!stop) timer = setTimeout(tick, 2000)
    }
    let timer = setTimeout(tick, 0)
    return () => {
      stop = true
      clearTimeout(timer)
    }
  }, [refresh])

  return (
    <Ctx.Provider value={{ state, devices, error, updatedAt, refresh }}>
      <WalletProvider tailnetRegistry={state?.tailnet?.registry}>{children}</WalletProvider>
    </Ctx.Provider>
  )
}
