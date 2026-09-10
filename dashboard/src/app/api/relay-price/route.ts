import { NextRequest } from "next/server";

// Data relays are discovered at runtime from sidecar's /relays (each with its
// own sidecarUrl), so unlike sidecar itself this destination can't be a
// static next.config.ts rewrite — it varies per relay. This route is the one
// place the dashboard proxies to an arbitrary, chain-discovered URL, so it's
// restricted to exactly the one path (relay-sidecar's GET /price) rather than
// forwarding an arbitrary path segment.
export async function GET(req: NextRequest) {
  const sidecarUrl = req.nextUrl.searchParams.get("url");
  if (!sidecarUrl) {
    return Response.json({ error: "missing url query param" }, { status: 400 });
  }

  let target: URL;
  try {
    target = new URL("/price", sidecarUrl);
  } catch {
    return Response.json({ error: "invalid url query param" }, { status: 400 });
  }
  if (target.protocol !== "http:" && target.protocol !== "https:") {
    return Response.json({ error: "url must be http(s)" }, { status: 400 });
  }

  try {
    const res = await fetch(target, { cache: "no-store" });
    const body = await res.json();
    return Response.json(body, { status: res.status });
  } catch (err) {
    return Response.json(
      { error: err instanceof Error ? err.message : String(err) },
      { status: 502 },
    );
  }
}
