import type { NextConfig } from 'next'

// Sidecar access goes through explicit GET-only Route Handlers under
// src/app/api/sidecar. A catch-all rewrite would also expose the sidecar's
// payment POST routes through this otherwise read-only dashboard.
const nextConfig: NextConfig = {}

export default nextConfig
