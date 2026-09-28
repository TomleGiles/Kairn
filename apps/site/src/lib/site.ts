// Paramètres du site vitrine : à ajuster avant la mise en ligne (ou via les
// variables NEXT_PUBLIC_* au moment du build). Les textes sont dans lib/content.
export const site = {
  name: "Kairn",
  /** URL canonique du site vitrine (sitemap, balises canonical et Open Graph). */
  url: (process.env.NEXT_PUBLIC_SITE_URL ?? "https://kairn.example.com").replace(/\/$/, ""),
  /** Application (connexion, documentation servie sous /docs). */
  appUrl: (process.env.NEXT_PUBLIC_APP_URL ?? "https://app.kairn.example.com").replace(/\/$/, ""),
  /** Adresse de contact commercial (demandes de démo). */
  contactEmail: process.env.NEXT_PUBLIC_CONTACT_EMAIL ?? "contact@kairn.example.com",
};

/** Lien de demande de démo, avec un objet de message dans la langue de la page. */
export function demoHref(subject: string): string {
  return `mailto:${site.contactEmail}?subject=${encodeURIComponent(subject)}`;
}

export const docsHref = `${site.appUrl}/docs`;
