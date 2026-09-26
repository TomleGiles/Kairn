// Paramètres du site vitrine : à ajuster avant la mise en ligne (ou via les
// variables NEXT_PUBLIC_* au moment du build).
export const site = {
  name: "Kairn",
  /** URL canonique du site vitrine (sitemap, balises canonical et Open Graph). */
  url: (process.env.NEXT_PUBLIC_SITE_URL ?? "https://kairn.example.com").replace(/\/$/, ""),
  /** Application (connexion, documentation servie sous /docs). */
  appUrl: (process.env.NEXT_PUBLIC_APP_URL ?? "https://app.kairn.example.com").replace(/\/$/, ""),
  /** Adresse de contact commercial (demandes de démo). */
  contactEmail: process.env.NEXT_PUBLIC_CONTACT_EMAIL ?? "contact@kairn.example.com",
  title: "Kairn — Observabilité des coûts pour clouds souverains, OpenStack et Kubernetes",
  description:
    "Kairn relie coûts, usage et déploiements sur OVHcloud, Scaleway, OUTSCALE, OpenStack et Kubernetes : combien vous dépensez, si c'est bien utilisé et pourquoi ça a bougé. FinOps souverain, hébergé dans l'UE ou en self-hosted.",
  keywords: [
    "FinOps",
    "FinOps souverain",
    "coûts cloud",
    "observabilité des coûts",
    "OVHcloud",
    "Scaleway",
    "OUTSCALE",
    "OpenStack",
    "Kubernetes",
    "showback",
    "chargeback",
    "rightsizing",
    "cloud souverain",
    "SecNumCloud",
  ],
};

export const demoHref = `mailto:${site.contactEmail}?subject=${encodeURIComponent("Demande de démo Kairn")}`;
export const docsHref = `${site.appUrl}/docs`;
