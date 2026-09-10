import type { NextConfig } from "next";

// sidecar is loopback-only by design (see sidecar/src/index.ts) and sets no
// CORS headers, so the browser can't call it directly from a different
// origin/port. Rewriting server-side avoids both problems without touching
// sidecar itself — the dashboard never becomes a second thing sidecar has to
// know about.
const SIDECAR_URL = process.env.SIDECAR_URL ?? "http://127.0.0.1:7890";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      { source: "/api/sidecar/:path*", destination: `${SIDECAR_URL}/:path*` },
    ];
  },
};

export default nextConfig;
