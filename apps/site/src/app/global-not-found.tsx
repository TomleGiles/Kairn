import type { Metadata } from "next";

import { Logo } from "@/components/icons";
import { homePath } from "@/lib/content";

import "./globals.css";

// Page 404 commune aux deux langues : le site a un layout racine par langue,
// donc aucune mise en page unique ne peut porter une page « introuvable ».
export const metadata: Metadata = {
  title: "404 — Kairn",
  robots: { index: false, follow: true },
};

export default function GlobalNotFound() {
  return (
    <html lang="fr">
      <body className="flex min-h-screen items-center justify-center bg-ink px-4 font-sans text-ink-fg">
        <main className="max-w-md text-center">
          <Logo className="mx-auto size-12" />
          <h1 className="mt-6 text-3xl font-semibold tracking-tight">Page introuvable</h1>
          <p className="mt-3 text-ink-muted">Cette page n&apos;existe pas ou a été déplacée.</p>
          <a href={homePath.fr} className="mt-6 inline-block rounded-full bg-teal-400 px-5 py-2.5 text-sm font-semibold text-[#04201c]">
            Retour à l&apos;accueil
          </a>
          <div lang="en" className="mt-10 border-t border-white/10 pt-6">
            <h2 className="text-lg font-semibold">Page not found</h2>
            <p className="mt-2 text-sm text-ink-muted">This page doesn&apos;t exist or has moved.</p>
            <a href={homePath.en} className="mt-4 inline-block text-sm font-semibold text-teal-300 underline underline-offset-4">
              Back to the English home page
            </a>
          </div>
        </main>
      </body>
    </html>
  );
}
