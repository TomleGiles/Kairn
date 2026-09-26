import Link from "next/link";

import { StackedBars, UsageLines } from "@/components/charts/charts";
import { Badge, Card, CardBody, CardHeader, EmptyState, PageHeader, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe, must } from "@/lib/api/server";
import { chartValue, formatDate, formatDay, formatMoney } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { resourceTypeLabel } from "@/lib/utils";

const USAGE: Record<string, { metrics: string[]; unit: "percent" | "cores" | "iops" | "raw" }> = {
  "compute.instance": { metrics: ["cpu.utilization", "mem.utilization"], unit: "percent" },
  host: { metrics: ["cpu.utilization", "mem.utilization"], unit: "percent" },
  "k8s.pod": { metrics: ["cpu.usage_cores", "cpu.request_cores"], unit: "cores" },
  "storage.volume": { metrics: ["disk.iops"], unit: "iops" },
  "k8s.workload": { metrics: ["k8s.replicas", "http.requests_per_sec"], unit: "raw" },
};

export default async function ResourcePage({ params }: { params: Promise<{ org: string; id: string }> }) {
  const { org, id } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const path = { org_id: org, id };
  const d = must(await client.GET("/api/v1/orgs/{org_id}/resources/{id}", { params: { path } }));
  const r = d.resource!;
  const cur = d.currency ?? "EUR";
  const usage = USAGE[r.type ?? ""];
  const now = new Date();
  const series = usage
    ? await client
        .GET("/api/v1/orgs/{org_id}/usage", {
          params: {
            path: { org_id: org },
            query: { resource_id: [id], metric: usage.metrics, from: new Date(now.getTime() - 14 * 86400e3).toISOString(), to: now.toISOString(), step: "1h", agg: "avg" },
          },
        })
        .then(maybe)
    : null;
  const events = await client
    .GET("/api/v1/orgs/{org_id}/events", { params: { path: { org_id: org }, query: { resource_id: id, from: new Date(now.getTime() - 60 * 86400e3).toISOString() } } })
    .then(maybe);
  const total = Object.values(d.cost_by_type_30d ?? {}).reduce((s, v) => s + chartValue(v), 0);
  return (
    <>
      <PageHeader
        title={r.name || r.external_id || id}
        description={`${resourceTypeLabel(r.type ?? "", locale)} · ${r.provider} · ${r.region} · ${r.external_id}`}
        actions={<Link className="text-sm text-brand hover:underline" href={`/o/${org}/resources`}>← {t.common.back}</Link>}
      />
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <Card className="xl:col-span-2">
          <CardHeader title={t.inventory.costByType} description={formatMoney(total.toFixed(2), cur, locale)} />
          <CardBody>
            <StackedBars
              label={t.inventory.costByType}
              categories={(d.daily_cost_30d ?? []).map((c) => formatDay(c.period ?? "", locale))}
              series={[{ name: t.common.total, values: (d.daily_cost_30d ?? []).map((c) => chartValue(c.amount)) }]}
              currency={cur}
              locale={locale}
              height={220}
            />
            <div className="mt-3 flex flex-wrap gap-2">
              {Object.entries(d.cost_by_type_30d ?? {}).map(([k, v]) => (
                <Badge key={k}>
                  {(t.costTypes as Record<string, string>)[k] ?? k} : {formatMoney(v, cur, locale)}
                </Badge>
              ))}
            </div>
          </CardBody>
        </Card>
        <Card>
          <CardHeader title={t.inventory.attributes} />
          <CardBody>
            <dl className="grid grid-cols-2 gap-x-3 gap-y-1.5 text-sm">
              {Object.entries(r.attributes ?? {}).map(([k, v]) => (
                <div key={k} className="contents">
                  <dt className="truncate text-muted">{k}</dt>
                  <dd className="truncate font-mono text-xs leading-5">{String(v)}</dd>
                </div>
              ))}
            </dl>
            {Object.keys(r.labels ?? {}).length ? (
              <>
                <h3 className="mb-2 mt-4 text-xs font-medium uppercase text-muted">{t.inventory.labels}</h3>
                <div className="flex flex-wrap gap-1">
                  {Object.entries(r.labels ?? {}).map(([k, v]) => (
                    <Badge key={k} tone="brand">
                      {k}={v}
                    </Badge>
                  ))}
                </div>
              </>
            ) : null}
          </CardBody>
        </Card>
      </div>
      {usage && series?.length ? (
        <Card className="mt-4">
          <CardHeader title="Usage (14 j)" />
          <CardBody>
            <UsageLines
              label={`Usage ${r.name}`}
              unit={usage.unit}
              series={series.map((s) => ({ name: s.metric ?? "", points: (s.points ?? []).map((p) => [p.ts ?? "", p.value ?? 0] as [string, number]) }))}
            />
          </CardBody>
        </Card>
      ) : null}
      <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader title={t.inventory.relations} />
          <CardBody className="grid grid-cols-2 gap-4 text-sm">
            {[
              { title: t.inventory.parents, items: d.parents ?? [] },
              { title: t.inventory.children, items: d.children ?? [] },
            ].map((g) => (
              <div key={g.title}>
                <h3 className="mb-2 text-xs font-medium uppercase text-muted">{g.title}</h3>
                <ul className="space-y-1">
                  {g.items.map((n) => (
                    <li key={n.id}>
                      <Link className="hover:underline" href={`/o/${org}/resources/${n.id}`}>
                        {n.name}
                      </Link>{" "}
                      <span className="text-xs text-subtle">{resourceTypeLabel(n.type ?? "", locale)}</span>
                    </li>
                  ))}
                  {!g.items.length ? <li className="text-subtle">—</li> : null}
                </ul>
              </div>
            ))}
          </CardBody>
        </Card>
        <Card>
          <CardHeader title={t.inventory.history} />
          <Table>
            <thead>
              <tr>
                <Th>{t.inventory.validFrom}</Th>
                <Th>{t.inventory.validTo}</Th>
                <Th>flavor / size</Th>
              </tr>
            </thead>
            <tbody>
              {[...(d.history ?? [])].reverse().map((v) => (
                <tr key={v.valid_from}>
                  <Td>{formatDate(v.valid_from, locale, true)}</Td>
                  <Td>{v.valid_to ? formatDate(v.valid_to, locale, true) : <Badge tone="success">{t.inventory.current}</Badge>}</Td>
                  <Td className="font-mono text-xs">{String(v.attributes?.["flavor"] ?? v.attributes?.["size_gb"] ?? v.attributes?.["cpu_request_cores"] ?? "—")}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      </div>
      <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader title={t.nav.recommendations} />
          <ul className="divide-y divide-border">
            {(d.recommendations ?? []).map((rec) => (
              <li key={rec.id} className="flex items-center justify-between gap-3 px-5 py-3 text-sm">
                <Link className="hover:underline" href={`/o/${org}/recommendations/${rec.id}`}>
                  {rec.title}
                </Link>
                <span className="tabular font-medium text-success">{formatMoney(rec.savings_monthly, rec.currency, locale)}</span>
              </li>
            ))}
          </ul>
          {!d.recommendations?.length ? <EmptyState title={t.common.noData} /> : null}
        </Card>
        <Card>
          <CardHeader title={t.inventory.events} />
          <ul className="divide-y divide-border">
            {(events ?? [])
              .slice(-10)
              .reverse()
              .map((e, i) => (
                <li key={i} className="px-5 py-2.5 text-sm">
                  <span className="text-xs text-subtle">{formatDate(e.ts, locale, true)}</span> — {e.title}
                </li>
              ))}
          </ul>
          {!events?.length ? <EmptyState title={t.common.noData} /> : null}
        </Card>
      </div>
    </>
  );
}
