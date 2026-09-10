"use client";

import { usePolling } from "@/hooks/use-polling";
import { MonoValue } from "@/components/mono-value";
import { StatusText } from "@/components/status-text";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { isAuthorized, parseExpiry, truncateHex } from "@/lib/format";
import { SEPOLIA_ETHERSCAN_URL } from "@/lib/config";
import type { DeviceRecord } from "@/lib/types";

interface DeviceRow {
  label: string;
  record: DeviceRecord | null;
  error: string | null;
}

async function fetchDevices(): Promise<DeviceRow[]> {
  const configRes = await fetch("/api/config", { cache: "no-store" });
  if (!configRes.ok) throw new Error(`config returned ${configRes.status}`);
  const { deviceLabels } = (await configRes.json()) as { deviceLabels: string[] };

  return Promise.all(
    deviceLabels.map(async (label): Promise<DeviceRow> => {
      try {
        const res = await fetch(`/api/sidecar/device/${label}`, {
          cache: "no-store",
        });
        if (!res.ok) {
          const body = await res.json().catch(() => ({}));
          throw new Error(body.error ?? `HTTP ${res.status}`);
        }
        return { label, record: await res.json(), error: null };
      } catch (err) {
        return {
          label,
          record: null,
          error: err instanceof Error ? err.message : String(err),
        };
      }
    }),
  );
}

export default function DevicesPage() {
  const devices = usePolling(fetchDevices, 5_000);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-medium text-foreground">Devices</h1>
        <p className="text-sm text-muted-foreground">
          Authorization is pubkey present, not revoked, and expiry in the
          future — the same rule brambled&apos;s admission loop enforces.
        </p>
      </div>

      {!devices.data && !devices.error && (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
        </div>
      )}

      {devices.data && devices.data.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No devices configured — set{" "}
          <code className="font-mono text-foreground">DEVICE_LABELS</code> in
          the dashboard&apos;s environment (comma-separated labels).
        </p>
      )}

      {devices.data && devices.data.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Label</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Pubkey</TableHead>
              <TableHead>Expiry</TableHead>
              <TableHead className="text-right">ACL grants</TableHead>
              <TableHead className="text-right">ACL granters</TableHead>
              <TableHead>Token</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {devices.data.map((row) => (
              <TableRow key={row.label}>
                <TableCell className="font-mono text-sm">
                  {row.label}
                </TableCell>
                {row.error || !row.record ? (
                  <TableCell colSpan={6}>
                    <StatusText tone="negative">
                      unresolved — {row.error}
                    </StatusText>
                  </TableCell>
                ) : (
                  <DeviceCells record={row.record} />
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {devices.error && (
        <p className="text-sm text-destructive">{devices.error}</p>
      )}
    </div>
  );
}

function DeviceCells({ record }: { record: DeviceRecord }) {
  const authorized = isAuthorized(record);
  const expiry = parseExpiry(record.expiry);

  return (
    <>
      <TableCell>
        {record.revoked ? (
          <StatusText tone="negative">revoked</StatusText>
        ) : authorized ? (
          <StatusText tone="positive">authorized</StatusText>
        ) : (
          <StatusText tone="neutral">not authorized</StatusText>
        )}
      </TableCell>
      <TableCell>
        <MonoValue value={record.pubkey ?? ""} display={truncateHex(record.pubkey)} />
      </TableCell>
      <TableCell>
        <span className={expiry.expired ? "text-destructive" : "text-foreground"}>
          {expiry.relative}
        </span>
      </TableCell>
      <TableCell className="text-right font-mono text-sm">
        {record.acl.length}
      </TableCell>
      <TableCell className="text-right font-mono text-sm">
        {record.aclGranters.length}
      </TableCell>
      <TableCell>
        <MonoValue
          value={record.tokenId}
          display={truncateHex(record.tokenId, 4, 4)}
          href={`${SEPOLIA_ETHERSCAN_URL}/search?q=${record.tokenId}`}
        />
      </TableCell>
    </>
  );
}
