// Fonctions communes des scénarios k6.
import http from "k6/http";

export const BASE = (__ENV.KAIRN_URL || "http://localhost:8080").replace(/\/$/, "");

/**
 * En-têtes d'authentification : jeton d'API (KAIRN_TOKEN) en priorité, sinon
 * connexion de démonstration (instance KAIRN_MODE=demo uniquement).
 */
export function authenticate() {
  if (__ENV.KAIRN_TOKEN) {
    return { headers: { Authorization: `Bearer ${__ENV.KAIRN_TOKEN}` }, cookies: {} };
  }
  const res = http.post(`${BASE}/api/v1/auth/login`, JSON.stringify({ email: __ENV.KAIRN_DEMO_EMAIL || "demo@kairn.local" }), {
    headers: { "Content-Type": "application/json" },
  });
  if (res.status !== 200) {
    throw new Error(`login failed: ${res.status} ${res.body}`);
  }
  const cookies = {};
  for (const [name, values] of Object.entries(res.cookies)) {
    cookies[name] = values[0].value;
  }
  return { headers: {}, cookies };
}

/** Organisation ciblée : KAIRN_ORG, sinon la première accessible. */
export function organization(auth) {
  if (__ENV.KAIRN_ORG) return __ENV.KAIRN_ORG;
  const res = http.get(`${BASE}/api/v1/orgs`, { headers: auth.headers, cookies: auth.cookies });
  const orgs = res.json() || []; // GET /orgs renvoie un tableau
  if (!orgs.length) throw new Error("no organization available");
  return orgs[0].id;
}

/** Date UTC RFC 3339 à minuit, décalée de `days` jours. */
export function day(days) {
  const d = new Date();
  d.setUTCHours(0, 0, 0, 0);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString();
}
