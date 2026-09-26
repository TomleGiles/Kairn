import type { NextConfig } from "next";

// Export statique : HTML pré-généré, hébergeable sur n'importe quel serveur ou CDN.
const nextConfig: NextConfig = {
  output: "export",
  trailingSlash: true,
  images: { unoptimized: true },
  poweredByHeader: false,
};

export default nextConfig;
