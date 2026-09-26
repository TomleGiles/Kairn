import { cookies } from "next/headers";

import type { Locale } from "@/lib/format";

import { en } from "./en";
import { fr, type Dictionary } from "./fr";

export type { Dictionary };

export const LOCALE_COOKIE = "kairn_locale";

const dictionaries: Record<Locale, Dictionary> = { fr, en };

export function getDictionary(locale: Locale): Dictionary {
  return dictionaries[locale] ?? fr;
}

/** Langue de la requête (cookie, français par défaut). */
export async function getLocale(): Promise<Locale> {
  const c = (await cookies()).get(LOCALE_COOKIE)?.value;
  return c === "en" ? "en" : "fr";
}

export async function getI18n(): Promise<{ locale: Locale; t: Dictionary }> {
  const locale = await getLocale();
  return { locale, t: getDictionary(locale) };
}
