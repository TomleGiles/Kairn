"use client";

import createClient from "openapi-fetch";

import type { paths } from "./schema";

/** Client typé côté navigateur (même origine : cookie de session). */
export const browserApi = createClient<paths>({ baseUrl: "", credentials: "include" });

/** Message d'erreur lisible depuis une réponse problem+json. */
export function problemMessage(error: unknown, fallback = "Erreur"): string {
  if (error && typeof error === "object") {
    const e = error as { detail?: string; title?: string; errors?: { message?: string; location?: string }[] };
    const first = e.errors?.[0];
    if (first?.message) return `${e.detail ?? e.title ?? fallback} (${first.location ?? ""} ${first.message})`.trim();
    return e.detail ?? e.title ?? fallback;
  }
  return fallback;
}
