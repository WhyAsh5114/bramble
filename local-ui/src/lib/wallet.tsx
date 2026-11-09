'use client'

// The browser wallet is how a person creates a tailnet and administers it.
// Every write is signed and paid for by that wallet; nothing is sponsored and
// no key ever reaches the daemon (ADR 0019).
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  createPublicClient,
  createWalletClient,
  custom,
  type Address,
  type EIP1193Provider,
  type Hash,
  type TransactionReceipt,
} from 'viem'
import { sepolia } from 'viem/chains'
import { api } from './api'

declare global {
  interface Window {
    ethereum?: EIP1193Provider
  }
}

export type TxStep = {
  label: string
  hash?: Hash
  status: 'waiting' | 'signing' | 'pending' | 'done' | 'failed'
  error?: string
}

interface WalletCtx {
  available: boolean
  address?: Address
  chainOk: boolean
  admin: boolean
  connecting: boolean
  error?: string
  connect: () => Promise<Address | undefined>
  disconnect: () => void
  /** Sends one write and waits for it to be mined. Throws if it reverts. */
  send: (request: Record<string, unknown>) => Promise<TransactionReceipt>
  balance?: bigint
  refreshAdmin: () => void
}

const Ctx = createContext<WalletCtx | null>(null)

export function useWallet() {
  const c = useContext(Ctx)
  if (!c) throw new Error('useWallet outside WalletProvider')
  return c
}

function provider(): EIP1193Provider | undefined {
  return typeof window === 'undefined' ? undefined : window.ethereum
}

export function WalletProvider({ tailnetRegistry, children }: { tailnetRegistry?: string; children: ReactNode }) {
  const [address, setAddress] = useState<Address>()
  const [chainId, setChainId] = useState<number>()
  const [admin, setAdmin] = useState(false)
  const [connecting, setConnecting] = useState(false)
  const [error, setError] = useState<string>()
  const [balance, setBalance] = useState<bigint>()
  const [adminTick, setAdminTick] = useState(0)
  const [available, setAvailable] = useState(false)

  useEffect(() => {
    const eth = provider()
    setAvailable(!!eth)
    if (!eth) return
    eth
      .request({ method: 'eth_accounts' })
      .then((a) => setAddress((a as Address[])[0]))
      .catch(() => {})
    eth
      .request({ method: 'eth_chainId' })
      .then((c) => setChainId(Number(c)))
      .catch(() => {})
    const onAccounts = (a: unknown) => setAddress((a as Address[])[0])
    const onChain = (c: unknown) => setChainId(Number(c))
    eth.on?.('accountsChanged', onAccounts as never)
    eth.on?.('chainChanged', onChain as never)
    return () => {
      eth.removeListener?.('accountsChanged', onAccounts as never)
      eth.removeListener?.('chainChanged', onChain as never)
    }
  }, [])

  useEffect(() => {
    if (!address || !tailnetRegistry) {
      setAdmin(false)
      return
    }
    let stop = false
    api
      .isAdmin(address)
      .then((r) => !stop && setAdmin(r.admin))
      .catch(() => !stop && setAdmin(false))
    return () => {
      stop = true
    }
  }, [address, tailnetRegistry, adminTick])

  useEffect(() => {
    const eth = provider()
    if (!eth || !address || chainId !== sepolia.id) return
    createPublicClient({ chain: sepolia, transport: custom(eth) })
      .getBalance({ address })
      .then(setBalance)
      .catch(() => {})
  }, [address, chainId, adminTick])

  const ensureChain = useCallback(async (eth: EIP1193Provider) => {
    const current = Number(await eth.request({ method: 'eth_chainId' }))
    if (current === sepolia.id) return
    try {
      await eth.request({ method: 'wallet_switchEthereumChain', params: [{ chainId: '0xaa36a7' }] })
    } catch {
      await eth.request({
        method: 'wallet_addEthereumChain',
        params: [
          {
            chainId: '0xaa36a7',
            chainName: 'Sepolia',
            nativeCurrency: { name: 'Sepolia Ether', symbol: 'ETH', decimals: 18 },
            rpcUrls: ['https://ethereum-sepolia-rpc.publicnode.com'],
            blockExplorerUrls: ['https://sepolia.etherscan.io'],
          },
        ],
      })
    }
    setChainId(sepolia.id)
  }, [])

  const connect = useCallback(async () => {
    const eth = provider()
    if (!eth) {
      setError('No browser wallet found. Install one (MetaMask, Rabby or Brave Wallet) and reload.')
      return undefined
    }
    setConnecting(true)
    setError(undefined)
    try {
      const accounts = (await eth.request({ method: 'eth_requestAccounts' })) as Address[]
      setAddress(accounts[0])
      await ensureChain(eth)
      return accounts[0]
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      return undefined
    } finally {
      setConnecting(false)
    }
  }, [ensureChain])

  const send = useCallback(
    async (request: Record<string, unknown>) => {
      const eth = provider()
      if (!eth) throw new Error('No browser wallet found.')
      const account = address ?? (await connect())
      if (!account) throw new Error('Connect a wallet first.')
      await ensureChain(eth)
      const wallet = createWalletClient({ account, chain: sepolia, transport: custom(eth) })
      const pub = createPublicClient({ chain: sepolia, transport: custom(eth) })
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const hash = await wallet.writeContract(request as any)
      const receipt = await pub.waitForTransactionReceipt({
        hash,
        pollingInterval: 3000,
        retryCount: 60,
        timeout: 600_000,
      })
      if (receipt.status !== 'success') throw new Error(`The transaction reverted: ${hash}`)
      setAdminTick((t) => t + 1)
      return receipt
    },
    [address, connect, ensureChain]
  )

  const value = useMemo<WalletCtx>(
    () => ({
      available,
      address,
      chainOk: chainId === sepolia.id,
      admin,
      connecting,
      error,
      connect,
      disconnect: () => setAddress(undefined),
      send,
      balance,
      refreshAdmin: () => setAdminTick((t) => t + 1),
    }),
    [available, address, chainId, admin, connecting, error, connect, send, balance]
  )
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

/** Runs a list of wallet writes in order and reports each step's progress. */
export async function runSteps(
  steps: { label: string; run: () => Promise<TransactionReceipt | void> }[],
  onUpdate: (s: TxStep[]) => void
) {
  const state: TxStep[] = steps.map((s) => ({ label: s.label, status: 'waiting' }))
  onUpdate([...state])
  for (let i = 0; i < steps.length; i++) {
    state[i] = { ...state[i], status: 'signing' }
    onUpdate([...state])
    try {
      const r = await steps[i].run()
      state[i] = { ...state[i], status: 'done', hash: r ? r.transactionHash : undefined }
      onUpdate([...state])
    } catch (e) {
      state[i] = { ...state[i], status: 'failed', error: e instanceof Error ? e.message.split('\n')[0] : String(e) }
      onUpdate([...state])
      throw e
    }
  }
}
