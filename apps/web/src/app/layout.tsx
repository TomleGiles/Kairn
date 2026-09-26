import type { Metadata, Viewport } from "next";
import { cookies } from "next/headers";

import { getI18n } from "@/lib/i18n";

import "./globals.css";

export const metadata: Metadata = {
  title: { default: "Kairn", template: "%s · Kairn" },
  description: "Observabilité orientée coûts pour les clouds souverains, OpenStack et Kubernetes.",
  robots: { index: false, follow: false },
};

export const viewport: Viewport = { width: "device-width", initialScale: 1 };

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const { locale, t } = await getI18n();
  const theme = (await cookies()).get("kairn_theme")?.value === "dark" ? "dark" : "";
  return (
    <html lang={locale} className={theme}>
      <body className="min-h-screen">
        <a href="#main" className="sr-only focus:not-sr-only focus:fixed focus:left-2 focus:top-2 focus:z-50 focus:rounded focus:bg-surface focus:px-3 focus:py-2">
          {t.common.skipToContent}
        </a>
        {children}
      </body>
    </html>
  );
}
