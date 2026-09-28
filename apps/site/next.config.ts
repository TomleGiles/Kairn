import type { NextConfig } from "next";

// Export statique : HTML pré-généré, hébergeable sur n'importe quel serveur ou CDN.
const nextConfig: NextConfig = {
  output: "export",
  trailingSlash: true,
  images: { unoptimized: true },
  poweredByHeader: false,
  // Un layout racine par langue (groupes (fr) et (en)) : la page 404 est définie
  // une seule fois dans app/global-not-found.tsx.
  experimental: { globalNotFound: true },
};

export default nextConfig;
