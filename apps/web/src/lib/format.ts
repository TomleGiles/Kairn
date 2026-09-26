// Formatage localisé. Les montants arrivent de l'API en chaînes décimales :
// ils sont formatés sans conversion en flottant (arrondi au centime exact).

export type Locale = "fr" | "en";

const SYMBOLS: Record<string, string> = { EUR: "€", USD: "$", GBP: "£", CHF: "CHF" };

/** Arrondit une chaîne décimale à `places` décimales (demi vers l'extérieur), sans flottant. */
export function roundDecimal(value: string, places = 2): { neg: boolean; int: string; frac: string } {
  let s = (value ?? "0").trim();
  let neg = false;
  if (s.startsWith("-")) {
    neg = true;
    s = s.slice(1);
  } else if (s.startsWith("+")) {
    s = s.slice(1);
  }
  let [int, frac = ""] = s.split(".");
  int = int.replace(/^0+(?=\d)/, "") || "0";
  frac = frac.padEnd(places + 1, "0");
  const keep = frac.slice(0, places);
  const next = frac.charCodeAt(places) - 48;
  let digits = (int + keep).split("").map((c) => c.charCodeAt(0) - 48);
  if (next >= 5) {
    let i = digits.length - 1;
    while (i >= 0) {
      if (digits[i] === 9) {
        digits[i] = 0;
        i--;
      } else {
        digits[i]++;
        break;
      }
    }
    if (i < 0) digits = [1, ...digits];
  }
  const all = digits.join("");
  const intPart = places > 0 ? all.slice(0, all.length - places) || "0" : all;
  const fracPart = places > 0 ? all.slice(all.length - places) : "";
  const isZero = /^0*$/.test(intPart + fracPart);
  return { neg: neg && !isZero, int: intPart.replace(/^0+(?=\d)/, ""), frac: fracPart };
}

function group(int: string, sep: string): string {
  return int.replace(/\B(?=(\d{3})+(?!\d))/g, sep);
}

/** Formate un montant décimal : « 1 234,56 € » (fr) ou « €1,234.56 » (en). */
export function formatMoney(value: string | undefined | null, currency = "EUR", locale: Locale = "fr", places = 2): string {
  const { neg, int, frac } = roundDecimal(value ?? "0", places);
  const sym = SYMBOLS[currency] ?? currency;
  if (locale === "fr") {
    const body = group(int, " ") + (places > 0 ? "," + frac : "");
    return `${neg ? "−" : ""}${body} ${sym}`;
  }
  const body = group(int, ",") + (places > 0 ? "." + frac : "");
  return `${neg ? "-" : ""}${sym}${body}`;
}

/** Montant compact pour les axes et tuiles (« 12,3 k€ »). Affichage uniquement. */
export function formatMoneyCompact(value: string | number, currency = "EUR", locale: Locale = "fr"): string {
  const n = typeof value === "number" ? value : Number(value);
  return new Intl.NumberFormat(locale === "fr" ? "fr-FR" : "en-GB", {
    style: "currency",
    currency,
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(n);
}

/** Convertit un montant en nombre pour le tracé des graphiques (jamais pour un calcul). */
export function chartValue(value: string | undefined | null): number {
  const n = Number(value ?? 0);
  return Number.isFinite(n) ? Math.round(n * 100) / 100 : 0;
}

export function formatPercent(value: string | number | undefined | null, locale: Locale = "fr", digits = 1, signed = false): string {
  const n = typeof value === "number" ? value : Number(value ?? 0);
  const s = new Intl.NumberFormat(locale === "fr" ? "fr-FR" : "en-GB", {
    maximumFractionDigits: digits,
    minimumFractionDigits: 0,
    signDisplay: signed ? "exceptZero" : "auto",
  }).format(n);
  return locale === "fr" ? `${s} %` : `${s}%`;
}

export function formatNumber(value: number | string | undefined | null, locale: Locale = "fr", digits = 2): string {
  const n = typeof value === "number" ? value : Number(value ?? 0);
  return new Intl.NumberFormat(locale === "fr" ? "fr-FR" : "en-GB", { maximumFractionDigits: digits }).format(n);
}

export function formatDate(value: string | Date | undefined | null, locale: Locale = "fr", withTime = false): string {
  if (!value) return "—";
  const d = typeof value === "string" ? new Date(value) : value;
  return new Intl.DateTimeFormat(locale === "fr" ? "fr-FR" : "en-GB", {
    day: "2-digit",
    month: "short",
    year: "numeric",
    ...(withTime ? { hour: "2-digit", minute: "2-digit" } : {}),
    timeZone: "Europe/Paris",
  }).format(d);
}

export function formatDay(value: string | Date, locale: Locale = "fr"): string {
  const d = typeof value === "string" ? new Date(value) : value;
  return new Intl.DateTimeFormat(locale === "fr" ? "fr-FR" : "en-GB", { day: "2-digit", month: "2-digit", timeZone: "UTC" }).format(d);
}

export function relativeTime(value: string | undefined | null, locale: Locale = "fr"): string {
  if (!value) return "—";
  const diff = (new Date(value).getTime() - Date.now()) / 1000;
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  const abs = Math.abs(diff);
  if (abs < 60) return rtf.format(Math.round(diff), "second");
  if (abs < 3600) return rtf.format(Math.round(diff / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), "hour");
  return rtf.format(Math.round(diff / 86400), "day");
}

/** Compare deux chaînes décimales (−1, 0, 1) sans flottant. */
export function compareDecimal(a: string, b: string): number {
  const ra = roundDecimal(a, 6);
  const rb = roundDecimal(b, 6);
  const sa = (ra.neg ? -1 : 1) as number;
  const sb = (rb.neg ? -1 : 1) as number;
  if (sa !== sb) return sa < sb ? -1 : 1;
  const ia = ra.int + ra.frac;
  const ib = rb.int + rb.frac;
  if (ia.length !== ib.length) return (ia.length < ib.length ? -1 : 1) * sa;
  return (ia < ib ? -1 : ia > ib ? 1 : 0) * sa;
}
