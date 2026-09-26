import "server-only";

import createClient from "openapi-fetch";
import { cookies } from "next/headers";
import { notFound, redirect } from "next/navigation";

import type { components, paths } from "./schema";

export type Schemas = components["schemas"];

const API_URL = process.env.KAIRN_API_URL ?? "http://localhost:8080";

/** Client typé côté serveur : la session de l'utilisateur est transmise à l'API. */
export async function api() {
  const jar = await cookies();
  const session = jar.get("kairn_session")?.value;
  return createClient<paths>({
    baseUrl: API_URL,
    headers: session ? { Authorization: `Bearer ${session}` } : {},
    cache: "no-store",
  });
}

type Result<T> = { data?: T; error?: unknown; response: Response };

/** Renvoie les données ou gère les erreurs courantes (401 → connexion, 404 → page introuvable). */
export function must<T>(res: Result<T>): T {
  if (res.response.status === 401) redirect("/login");
  if (res.response.status === 404) notFound();
  if (res.data === undefined) {
    const detail = (res.error as { detail?: string } | undefined)?.detail ?? res.response.statusText;
    throw new Error(`API ${res.response.status}: ${detail}`);
  }
  return res.data;
}

/** Comme must, mais renvoie null sur 402/403/404 (fonctionnalité non incluse ou droit absent). */
export function maybe<T>(res: Result<T>): T | null {
  if (res.response.status === 401) redirect("/login");
  if ([402, 403, 404, 503].includes(res.response.status)) return null;
  return must(res);
}
