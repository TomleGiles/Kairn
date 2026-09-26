// Scénario de charge de l'ingestion (CLAUDE.md §3) : 1 million de points par
// minute et par worker, via la passerelle OTLP/HTTP (JSON) de l'agent Kairn.
//
//   KAIRN_GATEWAY=http://localhost:8081 AGENT_TOKEN=<jeton du connecteur agent> k6 run test/load/ingestion.js
//
// POINTS_PER_REQUEST (défaut 2000), RATE (requêtes/s, défaut 10) et DURATION
// (défaut 2m) ajustent la charge : 2000 × 10 × 60 = 1,2 M points/min.
import http from "k6/http";
import { check } from "k6";
import { Counter } from "k6/metrics";

const GATEWAY = (__ENV.KAIRN_GATEWAY || "http://localhost:8081").replace(/\/$/, "");
const TOKEN = __ENV.AGENT_TOKEN;
const PER_REQUEST = Number(__ENV.POINTS_PER_REQUEST || 2000);
const RATE = Number(__ENV.RATE || 10);
const HOSTS = Number(__ENV.HOSTS || 200);
const METRICS = ["system.cpu.utilization", "system.memory.utilization", "system.disk.operations.rate", "system.network.io.receive.rate"];

const accepted = new Counter("points_accepted");

export const options = {
  scenarios: {
    ingestion: {
      executor: "constant-arrival-rate",
      rate: RATE,
      timeUnit: "1s",
      duration: __ENV.DURATION || "2m",
      preAllocatedVUs: 20,
      maxVUs: 100,
    },
  },
  thresholds: {
    // 1 M points / min = 16 667 points/s.
    points_accepted: [`rate>${Math.floor(1_000_000 / 60)}`],
    http_req_failed: ["rate<0.001"],
    "http_req_duration{kind:otlp}": ["p(95)<1000"],
  },
};

export function setup() {
  if (!TOKEN) throw new Error("AGENT_TOKEN is required (token of an « agent » connector)");
}

// payload construit une requête OTLP JSON : HOSTS hôtes × METRICS métriques.
function payload(iter) {
  const now = Date.now();
  const perHost = Math.max(1, Math.floor(PER_REQUEST / METRICS.length / Math.min(HOSTS, PER_REQUEST)));
  const hosts = Math.min(HOSTS, Math.ceil(PER_REQUEST / METRICS.length / perHost));
  const resourceMetrics = [];
  let count = 0;
  for (let h = 0; h < hosts && count < PER_REQUEST; h++) {
    const metrics = METRICS.map((name) => {
      const dataPoints = [];
      for (let i = 0; i < perHost && count < PER_REQUEST; i++, count++) {
        dataPoints.push({ timeUnixNano: String((now - i * 1000) * 1e6), asDouble: Math.random() });
      }
      return { name, gauge: { dataPoints } };
    });
    resourceMetrics.push({
      resource: { attributes: [{ key: "host.id", value: { stringValue: `load-${(iter + h) % HOSTS}` } }] },
      scopeMetrics: [{ scope: { name: "k6" }, metrics }],
    });
  }
  return { body: JSON.stringify({ resourceMetrics }), count };
}

export default function () {
  const { body, count } = payload(__ITER);
  const res = http.post(`${GATEWAY}/ingest/v1/otlp/v1/metrics`, body, {
    headers: { Authorization: `Bearer ${TOKEN}`, "Content-Type": "application/json" },
    tags: { kind: "otlp" },
  });
  if (check(res, { "OTLP accepted": (r) => r.status === 200 })) {
    accepted.add(count);
  }
}
