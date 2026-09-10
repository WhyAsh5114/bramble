export function truncateHex(hex: string | null, lead = 6, trail = 4): string {
  if (!hex) return "—";
  const clean = hex.startsWith("0x") ? hex : `0x${hex}`;
  if (clean.length <= lead + trail + 2) return clean;
  return `${clean.slice(0, lead + 2)}…${clean.slice(-trail)}`;
}

export interface ExpiryInfo {
  date: Date | null;
  expired: boolean;
  relative: string;
}

// expiry is a unix-seconds string in the on-chain text record (see
// brambled/admission/loop.go's expiryInFuture, which parses it the same way).
export function parseExpiry(expiry: string): ExpiryInfo {
  const secs = Number(expiry);
  if (!Number.isFinite(secs) || secs <= 0) {
    return { date: null, expired: true, relative: "no expiry set" };
  }
  const date = new Date(secs * 1000);
  const deltaMs = date.getTime() - Date.now();
  const expired = deltaMs <= 0;
  const abs = Math.abs(deltaMs);
  const days = Math.floor(abs / 86_400_000);
  const hours = Math.floor((abs % 86_400_000) / 3_600_000);
  const minutes = Math.floor((abs % 3_600_000) / 60_000);
  const parts: string[] = [];
  if (days > 0) parts.push(`${days}d`);
  if (days === 0 && hours > 0) parts.push(`${hours}h`);
  if (days === 0 && hours === 0) parts.push(`${minutes}m`);
  const magnitude = parts.length > 0 ? parts.join(" ") : "less than a minute";
  return {
    date,
    expired,
    relative: expired ? `expired ${magnitude} ago` : `expires in ${magnitude}`,
  };
}

export function isAuthorized(record: {
  pubkey: string | null;
  revoked: boolean;
  expiry: string;
}): boolean {
  if (!record.pubkey || record.revoked) return false;
  return !parseExpiry(record.expiry).expired;
}
