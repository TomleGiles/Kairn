import { en } from "./en";
import { fr } from "./fr";
import type { Content, Locale } from "./types";

export type { Content, IconName, Locale, Rich } from "./types";

export const locales: Locale[] = ["fr", "en"];

/** Chemin de la page d'accueil de chaque langue (le français est à la racine). */
export const homePath: Record<Locale, string> = { fr: "/", en: "/en/" };

export function getContent(locale: Locale): Content {
  return locale === "en" ? en : fr;
}
