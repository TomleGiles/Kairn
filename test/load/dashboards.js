// Scénario de charge des tableaux de bord (CLAUDE.md §3) : P95 < 1,5 s sur
// 12 mois de données pour une organisation de 10 000 ressources.
//
//   k6 run test/load/dashboards.js                          # instance de démo locale
//   KAIRN_URL=https://kairn.example KAIRN_TOKEN=kairn_… KAIRN_ORG=<id> k6 run test/load/dashboards.js
//
// VUS (défaut 20) et DURATION (défaut 3m) ajustent la charge.
import http from "k6/http";
import { check, group, sleep } from "k6";

import { authenticate, BASE, day, organization } from "./lib.js";

const VUS = Number(__ENV.VUS || 20);

export const options = {
  scenarios: {
    dashboards: {
      executor: "ramping-vus",
      stages: [
        { duration: "30s", target: VUS },
        { duration: __ENV.DURATION || "3m", target: VUS },
        { duration: "15s", target: 0 },
      ],
      gracefulRampDown: "10s",
    },
  },
  thresholds: {
    "http_req_duration{kind:dashboard}": ["p(95)<1500"],
    "http_req_duration{kind:explorer}": ["p(95)<1500"],
    http_req_failed: ["rate<0.01"],
    checks: ["rate>0.99"],
  },
};

export function setup() {
  const auth = authenticate();
  return { auth, org: organization(auth) };
}

function get(ctx, path, kind, name) {
  const res = http.get(`${BASE}/api/v1/orgs/${ctx.org}${path}`, {
    headers: ctx.auth.headers,
    cookies: ctx.auth.cookies,
    tags: { kind, name },
  });
  check(res, { [`${name} 200`]: (r) => r.status === 200 });
  return res;
}

export default function (ctx) {
  group("vue d'ensemble", () => {
    get(ctx, "/costs/summary", "dashboard", "summary");
    get(ctx, "/recommendations/summary", "dashboard", "reco-summary");
    get(ctx, "/budgets-status", "dashboard", "budgets");
    get(ctx, "/anomalies", "dashboard", "anomalies");
  });
  sleep(1);
  group("explorateur de coûts, 12 mois", () => {
    const range = `from=${encodeURIComponent(day(-365))}&to=${encodeURIComponent(day(1))}`;
    get(ctx, `/costs?${range}&granularity=month&group_by=provider`, "explorer", "costs-provider-12m");
    get(ctx, `/costs?${range}&granularity=day&group_by=cost_type`, "explorer", "costs-type-12m-daily");
    get(ctx, `/costs?${range}&granularity=month&group_by=allocation_node_id`, "explorer", "costs-allocation-12m");
    get(ctx, `/costs?from=${encodeURIComponent(day(-30))}&to=${encodeURIComponent(day(1))}&granularity=total&group_by=resource_id&limit=50`,
      "explorer", "costs-top-resources");
  });
  sleep(1);
  group("usage et prévisions", () => {
    get(ctx, "/efficiency?level=workload", "dashboard", "efficiency");
    get(ctx, "/forecast", "dashboard", "forecast");
  });
  sleep(1);
}
