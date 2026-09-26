import Link from "next/link";

import { ApiButton } from "@/components/actions";
import { StackedBars } from "@/components/charts/charts";
import { Badge, Card, CardBody, CardHeader, PageHeader, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe, must } from "@/lib/api/server";
import { chartValue, formatDate, formatDay, formatMoney } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { severityTone } from "@/lib/utils";

/**
 * Fenêtre du graphique : 28 jours avant l'anomalie, 14 jours après (sans
 * dépasser demain). Composant serveur : évalué une fois par requête.
 */
function chartWindow(windowStart?: string, windowEnd?: string): { start: string; end: string } {
  const now = Date.now();
  const start = new Date(new Date(windowStart ?? now).getTime() - 28 * 86400e3).toISOString();
  const end = new Date(Math.min(now + 86400e3, new Date(windowEnd ?? now).getTime() + 14 * 86400e3)).toISOString();
  return { start, end };
}

export default async function AnomalyPage({ params }: { params: Promise<{ org: string; id: string }> }) {
  const { org, id } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const a = must(await client.GET("/api/v1/orgs/{org_id}/anomalies/{id}", { params: { path: { org_id: org, id } } }));
  // Série concernée : reconstruite depuis l'explorateur de coûts.
  const key = a.series_key ?? "";
  const { start, end } = chartWindow(a.window_start ?? undefined, a.window_end ?? undefined);
  const filter: string[] = [];
  if (key.startsWith("cost:node:")) filter.push(`allocation_node_id:${key.slice(10)}`);
  if (key.startsWith("cost:provider:")) filter.push(`provider:${key.slice(14)}`);
  if (key.startsWith("cost:workload:")) {
    const [ns, wl] = key.slice(14).split("/");
    filter.push(`label:k8s.namespace:${ns}`, `label:k8s.workload:${wl}`);
  }
  const series = await client
    .GET("/api/v1/orgs/{org_id}/costs", { params: { path: { org_id: org }, query: { from: start, to: end, granularity: "day", filter } } })
    .then(maybe);
  const cur = a.currency ?? "EUR";
  return (
    <>
      <PageHeader title={a.title ?? ""} description={`${formatDate(a.window_start, locale)} → ${formatDate(a.window_end, locale)}`} actions={<Link className="text-sm text-brand hover:underline" href={`/o/${org}/anomalies`}>← {t.common.back}</Link>} />
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <Card className="xl:col-span-2">
          <CardHeader title={t.anomalies.explanation} />
          <CardBody className="space-y-4">
            <p className="text-sm leading-relaxed">{a.explanation}</p>
            <div className="flex flex-wrap gap-2">
              <Badge tone={severityTone(a.severity)}>{(t.anomalies.severity as Record<string, string>)[a.severity ?? "info"]}</Badge>
              <Badge>score {Number(a.score ?? 0).toFixed(1)}</Badge>
              <Badge tone="brand">{(t.anomalies.statuses as Record<string, string>)[a.status ?? "open"]}</Badge>
            </div>
            <div className="flex gap-2">
              {a.status !== "acknowledged" && a.status !== "resolved" ? (
                <ApiButton method="POST" path={`/api/v1/orgs/${org}/anomalies/${id}/status`} body={{ status: "acknowledged" }} size="sm" variant="secondary">
                  {t.anomalies.acknowledge}
                </ApiButton>
              ) : null}
              {a.status !== "resolved" ? (
                <ApiButton method="POST" path={`/api/v1/orgs/${org}/anomalies/${id}/status`} body={{ status: "resolved" }} size="sm">
                  {t.anomalies.resolve}
                </ApiButton>
              ) : null}
            </div>
            {a.explanation_sources?.length ? (
              <details className="text-xs text-muted">
                <summary className="cursor-pointer">{t.anomalies.sources}</summary>
                <ul className="mt-1 list-disc pl-4 font-mono">
                  {a.explanation_sources.map((s, i) => (
                    <li key={i}>{s}</li>
                  ))}
                </ul>
              </details>
            ) : null}
          </CardBody>
        </Card>
        <Card>
          <CardBody className="space-y-3">
            <div>
              <p className="text-xs uppercase text-muted">{t.anomalies.expected}</p>
              <p className="text-xl font-semibold tabular">{formatMoney(a.expected, cur, locale)}</p>
            </div>
            <div>
              <p className="text-xs uppercase text-muted">{t.anomalies.actual}</p>
              <p className="text-xl font-semibold tabular text-danger">{formatMoney(a.actual, cur, locale)}</p>
            </div>
          </CardBody>
        </Card>
      </div>
      {series?.rows?.length ? (
        <Card className="mt-4">
          <CardBody>
            <StackedBars
              label={a.title ?? ""}
              categories={series.rows.map((r) => formatDay(r.period ?? "", locale))}
              series={[{ name: t.common.total, values: series.rows.map((r) => chartValue(r.amount)) }]}
              currency={cur}
              locale={locale}
            />
          </CardBody>
        </Card>
      ) : null}
      <Card className="mt-4">
        <CardHeader title={t.anomalies.correlated} />
        <Table>
          <thead>
            <tr>
              <Th>Date</Th>
              <Th>{t.common.type}</Th>
              <Th>{t.common.name}</Th>
              <Th align="right">Score</Th>
            </tr>
          </thead>
          <tbody>
            {(a.correlated_events ?? []).map((c, i) => (
              <tr key={i}>
                <Td className="whitespace-nowrap">{formatDate(c.event?.ts, locale, true)}</Td>
                <Td>
                  <Badge>{c.event?.kind}</Badge>
                </Td>
                <Td>
                  <p>{c.event?.title}</p>
                  <p className="text-xs text-muted">{c.why}</p>
                </Td>
                <Td align="right">{Number(c.score ?? 0).toFixed(2)}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      </Card>
    </>
  );
}
