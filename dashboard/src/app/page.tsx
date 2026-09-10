"use client";

import Link from "next/link";
import { usePolling } from "@/hooks/use-polling";
import { MonoValue } from "@/components/mono-value";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import {
  ENS_EXPLORER_URL,
  SEPOLIA_ETHERSCAN_URL,
} from "@/lib/config";
import type { Relays, SidecarHealth } from "@/lib/types";

async function fetchHealth(): Promise<SidecarHealth> {
  const res = await fetch("/api/sidecar/health", { cache: "no-store" });
  if (!res.ok) throw new Error(`sidecar /health returned ${res.status}`);
  return res.json();
}

async function fetchConfig(): Promise<{ deviceLabels: string[] }> {
  const res = await fetch("/api/config", { cache: "no-store" });
  if (!res.ok) throw new Error(`config returned ${res.status}`);
  return res.json();
}

async function fetchRelays(): Promise<Relays> {
  const res = await fetch("/api/sidecar/relays", { cache: "no-store" });
  if (!res.ok) throw new Error(`sidecar /relays returned ${res.status}`);
  return res.json();
}

export default function OverviewPage() {
  const health = usePolling(fetchHealth, 10_000);
  const config = usePolling(fetchConfig, 30_000);
  const relays = usePolling(fetchRelays, 10_000);

  return (
    <div className="flex flex-col gap-10">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-medium text-foreground">Tailnet</h1>
        <p className="text-sm text-muted-foreground">
          A live, read-only view over one bramble tailnet, sourced entirely
          from on-chain state via this node&apos;s sidecar.
        </p>
      </div>

      <Separator />

      <dl className="grid grid-cols-1 gap-6 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <dt className="text-sm text-muted-foreground">Name</dt>
          <dd>
            {health.data ? (
              <span className="font-mono text-sm text-foreground">
                {health.data.tailnetName}
              </span>
            ) : health.error ? (
              <span className="text-sm text-destructive">
                sidecar unreachable — {health.error}
              </span>
            ) : (
              <Skeleton className="h-5 w-40" />
            )}
          </dd>
        </div>

        <div className="flex flex-col gap-1.5">
          <dt className="text-sm text-muted-foreground">Registry</dt>
          <dd>
            {health.data ? (
              <MonoValue
                value={health.data.tailnetRegistry}
                display={health.data.tailnetRegistry}
                href={`${SEPOLIA_ETHERSCAN_URL}/address/${health.data.tailnetRegistry}`}
              />
            ) : (
              <Skeleton className="h-5 w-64" />
            )}
          </dd>
        </div>

        <div className="flex flex-col gap-1.5">
          <dt className="text-sm text-muted-foreground">Devices watched</dt>
          <dd>
            {config.data ? (
              <Link
                href="/devices"
                className="text-sm text-foreground underline decoration-border underline-offset-4 hover:decoration-foreground"
              >
                {config.data.deviceLabels.length}
              </Link>
            ) : (
              <Skeleton className="h-5 w-8" />
            )}
          </dd>
        </div>

        <div className="flex flex-col gap-1.5">
          <dt className="text-sm text-muted-foreground">Relays discovered</dt>
          <dd>
            {relays.data ? (
              <Link
                href="/relays"
                className="text-sm text-foreground underline decoration-border underline-offset-4 hover:decoration-foreground"
              >
                {relays.data.rendezvous.length} rendezvous ·{" "}
                {relays.data.dataRelays.length} data
              </Link>
            ) : (
              <Skeleton className="h-5 w-24" />
            )}
          </dd>
        </div>
      </dl>

      <Separator />

      <div className="flex flex-col gap-2">
        <h2 className="text-sm text-muted-foreground">On-chain tools</h2>
        <div className="flex flex-col gap-1">
          <a
            href={ENS_EXPLORER_URL}
            target="_blank"
            rel="noreferrer"
            className="text-sm text-foreground underline decoration-border underline-offset-4 hover:decoration-foreground"
          >
            Hackathon ENSv2 explorer ↗
          </a>
          <a
            href={SEPOLIA_ETHERSCAN_URL}
            target="_blank"
            rel="noreferrer"
            className="text-sm text-foreground underline decoration-border underline-offset-4 hover:decoration-foreground"
          >
            Sepolia Etherscan ↗
          </a>
        </div>
      </div>
    </div>
  );
}
