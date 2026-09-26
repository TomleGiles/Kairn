import type { NextConfig } from "next";

const api = process.env.KAIRN_API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  // Pas de génération automatique de fichiers d'instructions pour agents IA.
  agentRules: false,
  poweredByHeader: false,
  // L'API est servie sous la même origine : cookies de session partagés, pas de CORS.
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${api}/api/:path*` },
      { source: "/ingest/:path*", destination: `${api}/ingest/:path*` },
      { source: "/scim/:path*", destination: `${api}/scim/:path*` },
      // Documentation statique générée par apps/docs dans public/docs.
      { source: "/docs", destination: "/docs/index.html" },
      { source: "/docs/:path*", destination: "/docs/:path*.html" },
    ];
  },
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
        ],
      },
    ];
  },
};

export default nextConfig;
