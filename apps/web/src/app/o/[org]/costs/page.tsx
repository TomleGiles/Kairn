import Link from "next/link";

import { StackedBars } from "@/components/charts/charts";
import { Badge, Card, CardBody, CardHeader, EmptyState, PageHeader, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe, must, type Schemas } from "@/lib/api/server";
import { chartValue, formatDay, formatMoney, formatPercent } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { nodeIndex } from "@/lib/nodes";

import { CostControls } from "./controls";

const DIMS = ["provider", "resource_type", "region", "cost_type", "allocation_node_id", "resource_id", "connector_id", "source", "sku"] as const;

function range(preset: string, now = new Date()): { from: string; to: string } {
  const day = (d: Date) => d.toISOString().slice(0, 10) + "T00:00:00Z";
  const tomorrow = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1));
  switch (preset) {
    case "90d":
      return { from: day(new Date(tomorrow.getTime() - 90 * 86400e3)), to: day(tomorrow) };
    case "month":
      return { from: day(new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1))), to: day(tomorrow) };
    case "lastmonth":
      return {
        from: day(new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - 1, 1))),
        to: day(new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1))),
      };
    default:
      return { from: day(new Date(tomorrow.getTime() - 30 * 86400e3)), to: day(tomorrow) };
  }
}

export default async function CostsPage({
  params,
  searchParams,
}: {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ group?: string; gran?: string; range?: string; filter?: string | string[] }>;
}) {
  const { org } = await params;
  const sp = await searchParams;
  const { locale, t } = await getI18n();
  const client = await api();
  const group = DIMS.includes((sp.group ?? "") as (typeof DIMS)[number]) ? (sp.group as string) : "cost_type";
  const gran = ["day", "week", "month"].includes(sp.gran ?? "") ? (sp.gran as "day" | "week" | "month") : "day";
  const preset = sp.range ?? "30d";
  const { from, to } = range(preset);
  const filters = ([] as string[]).concat(sp.filter ?? []);
  const path = { org_id: org };
  const [res, nodes, recon, conns] = await Promise.all([
    client
      .GET("/api/v1/orgs/{org_id}/costs", { params: { path, query: { from, to, granularity: gran, group_by: [group], filter: filters } } })
      .then(must),
    client.GET("/api/v1/orgs/{org_id}/allocation/nodes", { params: { path, query: { limit: 500 } } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/reconciliation", { params: { path } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/connectors", { params: { path } }).then(maybe),
  ]);
  const cur = res.currency ?? "EUR";
  const idx = nodeIndex(nodes?.items ?? [], t.common.unallocated);
  const connName = new Map((conns ?? []).map((c) => [c.id ?? "", c.name ?? ""]));
  const label = (v: string | undefined) => {
    if (group === "allocation_node_id") return idx.path(v);
    if (group === "cost_type") return (t.costTypes as Record<string, string>)[v ?? ""] ?? v ?? "—";
    if (group === "connector_id") return connName.get(v ?? "") || v || "—";
    return v || "—";
  };
  const rows: Schemas["CostRow"][] = res.rows ?? [];
  const periods = Array.from(new Set(rows.map((r) => r.period ?? ""))).sort();
  const totals = new Map<string, number>();
  for (const r of rows) {
    const k = r.keys?.[group] ?? "";
    totals.set(k, (totals.get(k) ?? 0) + chartValue(r.amount));
  }
  const keys = Array.from(totals.entries())
    .sort((a, b) => b[1] - a[1])
    .map(([k]) => k);
  const top = keys.slice(0, 8);
  const series = top.map((k) => ({
    name: label(k),
    values: periods.map((p) => chartValue(rows.find((r) => r.period === p && (r.keys?.[group] ?? "") === k)?.amount)),
  }));
  if (keys.length > 8) {
    series.push({
      name: locale === "fr" ? "Autres" : "Others",
      values: periods.map((p) => rows.filter((r) => r.period === p && !top.includes(r.keys?.[group] ?? "")).reduce((s, r) => s + chartValue(r.amount), 0)),
    });
  }
  // Tableau : totaux exacts par clé (sommes décimales côté API, par période).
  const byKey = new Map<string, Schemas["CostRow"][]>();
  for (const r of rows) byKey.set(r.keys?.[group] ?? "", [...(byKey.get(r.keys?.[group] ?? "") ?? []), r]);
  const exportQuery = new URLSearchParams({ from, to, format: "csv" });
  filters.forEach((f) => exportQuery.append("filter", f));
  const dimLabels = t.costs.dims as Record<string, string>;
  return (
    <>
      <PageHeader
        title={t.costs.title}
        description={`${formatMoney(res.total, cur, locale)} · ${formatDay(from, locale)} → ${formatDay(new Date(new Date(to).getTime() - 86400e3), locale)}`}
        actions={
          <>
            <a className="text-sm text-brand hover:underline" href={`/api/v1/orgs/${org}/costs/export?${exportQuery}`}>
              {t.costs.exportCsv}
            </a>
            <a className="text-sm text-brand hover:underline" href={`/api/v1/orgs/${org}/costs/export?${exportQuery.toString().replace("format=csv", "format=parquet")}`}>
              {t.costs.exportParquet}
            </a>
          </>
        }
      />
      <CostControls
        dims={DIMS.map((d) => ({ value: d, label: dimLabels[d] ?? d }))}
        group={group}
        gran={gran}
        preset={preset}
        filters={filters}
        labels={{
          groupBy: t.costs.groupBy,
          granularity: t.costs.granularity,
          period: t.costs.period,
          day: t.costs.day,
          week: t.costs.week,
          month: t.costs.month,
          last30: t.costs.last30,
          last90: t.costs.last90,
          thisMonth: t.costs.thisMonth,
          lastMonth: t.costs.lastMonth,
          filter: t.common.filter,
        }}
      />
      <Card className="mt-4">
        <CardBody>
          {periods.length ? (
            <StackedBars
              label={`${t.costs.title} — ${dimLabels[group]}`}
              categories={periods.map((p) => formatDay(p, locale))}
              series={series}
              currency={cur}
              locale={locale}
              height={320}
            />
          ) : (
            <EmptyState title={t.common.noData} />
          )}
        </CardBody>
      </Card>
      <Card className="mt-4">
        <CardHeader title={dimLabels[group]} />
        <Table>
          <thead>
            <tr>
              <Th>{dimLabels[group]}</Th>
              <Th align="right">{t.common.total}</Th>
              <Th align="right">%</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {keys.map((k) => {
              const rs = byKey.get(k) ?? [];
              const sum = rs.reduce((s, r) => s + chartValue(r.amount), 0);
              const totalNum = chartValue(res.total);
              const filterLink = new URLSearchParams({ group, gran, range: preset });
              [...filters, `${group}:${k}`].forEach((f) => filterLink.append("filter", f));
              return (
                <tr key={k}>
                  <Td>
                    {group === "resource_id" && k ? (
                      <Link className="hover:underline" href={`/o/${org}/resources/${k}`}>
                        {k.slice(0, 8)}
                      </Link>
                    ) : (
                      label(k)
                    )}
                  </Td>
                  <Td align="right">{formatMoney(sum.toFixed(2), cur, locale)}</Td>
                  <Td align="right" className="text-muted">
                    {totalNum ? formatPercent((sum / totalNum) * 100, locale) : "—"}
                  </Td>
                  <Td align="right">
                    <Link className="text-xs text-brand hover:underline" href={`?${filterLink}`}>
                      {t.common.filter}
                    </Link>
                  </Td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      </Card>
      {recon?.length ? (
        <Card className="mt-4">
          <CardHeader title={t.costs.reconciliation} description="Objectif : écart < 2 %" />
          <Table>
            <thead>
              <tr>
                <Th>{t.costs.month}</Th>
                <Th>{t.common.provider}</Th>
                <Th align="right">{t.costs.estimated}</Th>
                <Th align="right">{t.costs.billed}</Th>
                <Th align="right">{t.costs.delta}</Th>
              </tr>
            </thead>
            <tbody>
              {recon.map((r) => {
                const d = Math.abs(Number(r.delta_percent ?? 0));
                return (
                  <tr key={`${r.connector_id}-${r.month}`}>
                    <Td>{(r.month ?? "").slice(0, 7)}</Td>
                    <Td>{connName.get(r.connector_id ?? "") ?? r.provider}</Td>
                    <Td align="right">{formatMoney(r.estimated, r.currency, locale)}</Td>
                    <Td align="right">{formatMoney(r.billed, r.currency, locale)}</Td>
                    <Td align="right">
                      <Badge tone={d < 2 ? "success" : d < 5 ? "warning" : "danger"}>{formatPercent(r.delta_percent, locale, 2, true)}</Badge>
                    </Td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        </Card>
      ) : null}
    </>
  );
}
