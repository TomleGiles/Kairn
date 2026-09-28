import type { Metadata, Viewport } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";

import { getContent, homePath, locales, type Locale } from "@/lib/content";
import { site } from "@/lib/site";

import "@/app/globals.css";

// Coquille HTML commune aux deux langues : chaque langue a son propre layout
// racine (groupes de routes (fr) et (en)) pour que <html lang> soit exact.

// Polices auto-hébergées à la construction : aucun appel à Google chez le visiteur.
const inter = Inter({ subsets: ["latin"], variable: "--font-inter", display: "swap" });
const mono = JetBrains_Mono({ subsets: ["latin"], variable: "--font-mono-face", display: "swap" });

const ogPath: Record<Locale, string> = { fr: "/og.png", en: "/en/og.png" };

export function buildMetadata(locale: Locale): Metadata {
  const c = getContent(locale);
  const others = locales.filter((l) => l !== locale).map((l) => getContent(l).meta.ogLocale);
  return {
    metadataBase: new URL(site.url),
    title: { default: c.meta.title, template: `%s — ${site.name}` },
    description: c.meta.description,
    keywords: c.meta.keywords,
    applicationName: site.name,
    alternates: {
      canonical: homePath[locale],
      languages: { fr: homePath.fr, en: homePath.en, "x-default": homePath.fr },
    },
    openGraph: {
      type: "website",
      locale: c.meta.ogLocale,
      alternateLocale: others,
      url: homePath[locale],
      siteName: site.name,
      title: c.meta.title,
      description: c.meta.description,
      images: [{ url: ogPath[locale], width: 1200, height: 630, alt: c.meta.ogAlt }],
    },
    twitter: { card: "summary_large_image", title: c.meta.title, description: c.meta.description, images: [ogPath[locale]] },
    robots: { index: true, follow: true },
    formatDetection: { telephone: false },
  };
}

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#ffffff" },
    { media: "(prefers-color-scheme: dark)", color: "#070b12" },
  ],
};

export function Document({ locale, children }: { locale: Locale; children: React.ReactNode }) {
  const c = getContent(locale);
  return (
    <html lang={c.htmlLang} className={`${inter.variable} ${mono.variable}`}>
      <body className="font-sans">
        <a
          href="#contenu"
          className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-brand focus:px-3 focus:py-2 focus:text-brand-fg"
        >
          {c.ui.skip}
        </a>
        {children}
      </body>
    </html>
  );
}
