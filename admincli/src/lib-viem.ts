// Real-chain implementations of lib.ts's Resolver and Signer, plus the Key
// Ring-backed granter key loader. The CLIs use these. Importing this file loads
// .env through setup.ts, so tests and other importers that supply their own
// Resolver and Signer import lib.ts instead.
import { readFileSync } from 'node:fs'
import { createWalletClient, http, toHex, type Account } from 'viem'
import { packetToBytes } from 'viem/ens'
import { RPC_URL, tailnetName } from '../../sidecar/src/ens/config'
import { accountFromEnv, hackathonSepolia, publicClient } from './setup'
import { resolverWriteAbi } from './roles'
import { ringDecrypt } from './ledger-ring'
import { GRANTER_KEY_RING_PATH } from './granter-key-path'
import type { LibDeps, LoadGranterKey, Resolver, Signer } from './lib'

export const viemResolver: Resolver = {
  getText: (name, key) => publicClient.getEnsText({ name, key }),
}

/** Signs with the account from `accountEnvVar`. Reads the env var on the first write, not at construction. */
export function createViemSigner(account?: Account): Signer {
  let wallet: ReturnType<typeof createWalletClient> | undefined
  return {
    async setText(name, key, value) {
      wallet ??= createWalletClient({
        account: account ?? accountFromEnv(),
        chain: hackathonSepolia,
        transport: http(RPC_URL),
      })
      const resolverAddress = await publicClient.getEnsResolver({ name })
      if (!resolverAddress) throw new Error(`no resolver found for ${name}`)
      const hash = await wallet.writeContract({
        chain: hackathonSepolia,
        account: wallet.account!,
        address: resolverAddress,
        abi: resolverWriteAbi,
        functionName: 'setText',
        args: [toHex(packetToBytes(name)), key, value],
      })
      return {
        hash,
        async wait() {
          const receipt = await publicClient.waitForTransactionReceipt({ hash })
          if (receipt.status !== 'success') throw new Error('setText reverted on-chain')
        },
      }
    },
  }
}

/** Decrypts the granter's X25519 key from the Key Ring file. Run provision-granter-key first. */
export const loadGranterKeyFromRing: LoadGranterKey = async () => {
  let ciphertext: Buffer
  try {
    ciphertext = readFileSync(GRANTER_KEY_RING_PATH)
  } catch {
    throw new Error(
      `no ring-encrypted granter key at ${GRANTER_KEY_RING_PATH} — run ` +
        `\`BRAMBLE_GRANTER_PRIVATE_KEY=<hex> bun run provision-granter-key\` once first`
    )
  }
  return ringDecrypt(ciphertext)
}

/** The deps every CLI passes to lib.ts: real chain, `SEPOLIA_PRIVATE_KEY`, console output. */
export function cliDeps(): LibDeps {
  return { resolver: viemResolver, signer: createViemSigner(), tailnetName: tailnetName(), log: console.log }
}
