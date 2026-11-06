import type { PublicClient } from 'viem'
import { normalize } from 'viem/ens'
import { tailnetName } from '../../sidecar/src/ens/config'
import { listDeviceLabels } from '../../sidecar/src/ens/devices'

// The mesh-ip records already in use by every enrolled device except
// deviceLabel. client is the administrator's direct Sepolia client: the CLI
// never starts Tor, so it must not reach for the sidecar's Tor-only client.
export async function usedMeshIPs(
  client: PublicClient,
  deviceLabel: string,
  readMeshIP: (client: PublicClient, label: string) => Promise<string | null> = (c, label) =>
    c.getEnsText({ name: normalize(`${label}.${tailnetName()}`), key: 'mesh-ip' })
): Promise<(string | null)[]> {
  const otherLabels = (await listDeviceLabels(client)).filter((l) => l !== deviceLabel)
  return Promise.all(otherLabels.map((l) => readMeshIP(client, l)))
}
