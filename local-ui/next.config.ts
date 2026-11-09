import type { NextConfig } from 'next'

const dev = process.env.NODE_ENV === 'development'

const config: NextConfig = {
  // The export lands in `out/`; `pnpm embed` copies it to `node-daemon/ui/dist/` for go:embed.
  // In `pnpm dev`, /api goes to a running `bramble up` (BRAMBLE_API, default http://127.0.0.1:7777).
  ...(dev
    ? {
        async rewrites() {
          return [
            { source: '/api/:path*', destination: `${process.env.BRAMBLE_API ?? 'http://127.0.0.1:7777'}/api/:path*` },
          ]
        },
      }
    : { output: 'export' as const }),
  poweredByHeader: false,
  images: { unoptimized: true },
}

export default config
