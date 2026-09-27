import type { NextConfig } from "next";

// Where the Go API runs. Read only on the server; the browser never sees it.
const backendUrl = process.env.BACKEND_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  // Proxy /api/* to Go, so the browser only ever talks to this origin. That
  // lets the session live in an httpOnly cookie on the same origin as the
  // app, lets an OAuth provider redirect back to /api/auth/*, and removes the
  // need for CORS.
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${backendUrl}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
