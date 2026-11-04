import { fileURLToPath } from "node:url";

// Static export: `NEXT_EXPORT=1 next build` produces a pure static directory in web/out that can be
// dropped straight into an nginx web root. Development (next dev) does not set this variable, so the
// /api reverse proxy and hot reloading are kept.
const isExport = process.env.NEXT_EXPORT === "1";
// Vercel demo: the whole site runs on mock data with no backend, so no /api reverse proxy is needed.
const isMock = process.env.NEXT_PUBLIC_MOCK === "1";

/** @type {import('next').NextConfig} */
const nextConfig = {
  // Keep a lockfile in a parent directory from affecting root inference and asset path generation.
  turbopack: { root: fileURLToPath(new URL(".", import.meta.url)) },
  reactCompiler: true,
  // Allow LAN IPs to access dev resources (HMR); add or remove entries as needed.
  // During dev, allow any IPv4 origin to access /_next/* and HMR (unaffected by LAN IP changes).
  // Note: for security reasons Next forbids a bare "*"; use segment wildcards instead. "*.*.*.*" matches any IPv4.
  allowedDevOrigins: ["*.*.*.*"],
  compiler: {
    removeConsole: process.env.NODE_ENV === "production",
  },
  ...(isExport
    ? {
        // Pure static export: no Node runtime; images are not optimized; each route emits <route>/index.html.
        output: "export",
        images: { unoptimized: true },
        trailingSlash: true,
      }
    : isMock
      ? {
          // Vercel mock demo: no backend, so no /api reverse proxy is needed.
          images: { unoptimized: true },
        }
      : {
          // Development: proxy /api/* to the Go backend (default :8787, overridable with AUTOPENTEST_API).
          async rewrites() {
            const backend = process.env.AUTOPENTEST_API ?? "http://localhost:8787";
            return [{ source: "/api/:path*", destination: `${backend}/api/:path*` }];
          },
        }),
};

export default nextConfig;
