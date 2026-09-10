// Shared by provision-granter-key.ts (writer) and set-acl.ts (reader) so
// both agree on where the ring-encrypted granter key lives. Not committed
// — see admincli/.gitignore — since it's bound to one specific admin's
// Key Ring membership on one specific machine, not portable evidence.
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

export const GRANTER_KEY_RING_PATH =
  process.env.BRAMBLE_GRANTER_KEY_RING_PATH ?? path.resolve(__dirname, '../granter-key.ring')
