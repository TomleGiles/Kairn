// Structure du contenu éditorial, commune à toutes les langues : le compilateur
// signale toute clé oubliée dans une traduction.

export type Locale = "fr" | "en";

export type IconName =
  | "layers"
  | "wand"
  | "pulse"
  | "target"
  | "cube"
  | "server"
  | "chat"
  | "report"
  | "shield"
  | "plug"
  | "globe"
  | "lock"
  | "users"
  | "code";

/** Texte enrichi minimal : segments simples, en gras ou en code. */
export type Rich = (string | { b: string } | { code: string })[];

type Link = { href: string; label: string };
type Card = { icon: IconName; title: string; text: string };

export type Content = {
  locale: Locale;
  /** Langue BCP 47 du document et locale de formatage des nombres. */
  htmlLang: string;
  numberLocale: string;
  meta: {
    title: string;
    description: string;
    keywords: string[];
    ogLocale: string;
    ogAlt: string;
    ogTitle: string;
    ogTagline: string[];
  };
  ui: {
    skip: string;
    home: string;
    mainNav: string;
    footerNav: string;
    menu: string;
    nav: Link[];
    docs: string;
    login: string;
    demo: string;
    demoSubject: string;
    /** Lien vers l'autre langue. */
    otherLocale: { href: string; hrefLang: string; label: string; aria: string };
    hero: { badge: string; titleA: string; titleB: string; text: string; secondary: string; checks: string[]; scroll: string };
    sourcesTitle: string;
    problem: { eyebrow: string; titleA: string; titleB: string; text: string };
    cascade: { eyebrow: string; titleA: string; titleB: string; text: string };
    product: { eyebrow: string; title: string; text: string; bullets: string[][] };
    features: { eyebrow: string; title: string };
    ai: { eyebrow: string; title: string; text: string; bullets: string[]; aria: string; question: string; answer: Rich[]; action: Rich; sources: string; caption: string };
    personas: { eyebrow: string; title: string };
    steps: { eyebrow: string; title: string };
    sovereignty: { eyebrow: string; titleA: string; titleB: string; text: string };
    security: { eyebrow: string; title: string };
    integrations: { eyebrow: string; title: string };
    pricing: { eyebrow: string; title: string; text: string; highlight: string; contact: string; demo: string; planSr: string };
    faqTitle: string;
    cta: { titleA: string; titleB: string; text: string; docs: string };
    footer: {
      tagline: string;
      product: string;
      howItWorks: string;
      features: string;
      assistant: string;
      pricing: string;
      resources: string;
      connectors: string;
      securityLink: string;
      contact: string;
      rights: string;
    };
  };
  mock: {
    aria: string;
    windowTitle: string;
    sidebar: string[];
    kpis: { label: string; value: string; sub: string; tone: "warn" | "muted" | "good" }[];
    chartTitle: string;
    chartRange: string;
    legend: string[];
    anomalyTitle: string;
    anomalyHeadline: string;
    anomalyText: Rich;
    recoLabel: string;
    recoText: string;
    recoSaving: string;
    chipAccepted: string;
    chipCorrelated: string;
    caption: string;
  };
  visuals: {
    demoNote: string;
    invoiceTitle: string;
    invoiceBadge: string;
    invoiceTotalLabel: string;
    invoiceQuestion: string;
    kairnTitle: string;
    kairnBadge: string;
    kairnHighlight: Rich;
    drilldownAria: string;
    allocationAria: string;
    allocationTitle: string;
    allocationCoverage: string;
    allocationTotal: string;
    rightsizing: {
      aria: string;
      replicas: string;
      badge: string;
      cpu: string;
      memory: string;
      usedOf: string;
      reserved: string;
      memoryUnit: string;
      comment: string;
      saving: string;
      risk: string;
      measured: string;
    };
    correlation: { aria: string; title: string; badge: string; events: { time: string; label: string; source: string }[] };
  };
  sources: string[];
  questions: { kicker: string; title: string; text: string }[];
  features: Card[];
  personas: { role: string; need: string; points: string[] }[];
  steps: { n: string; title: string; text: string }[];
  sovereignty: Card[];
  security: string[];
  integrations: Card[];
  plans: { name: string; target: string; price: string; unit: string; features: string[]; highlight: boolean; contact: boolean }[];
  faq: { q: string; a: string }[];
  invoiceLines: { label: string; amount: string }[];
  invoiceTotal: string;
  teamCosts: { team: string; amount: string; share: number; color: string; note: string }[];
  drilldown: { level: string; name: string; amount: string; share: number; source: string; detail: string }[];
};
