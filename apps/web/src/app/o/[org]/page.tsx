import Link from "next/link";

import { Donut, StackedBars } from "@/components/charts/charts";
import { Badge, Card, CardBody, CardHeader, EmptyState, PageHeader, Progress, Stat, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe, must } from "@/lib/api/server";
import { chartValue, compareDecimal, formatDay, formatMoney, formatPercent } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { nodeIndex } from "@/lib/nodes";
import { resourceTypeLabel } from "@/lib/utils";

export default async function Overview({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const path = { org_id: org };
  const [summary, budgets, recos, anomalies, nodes] = await Promise.all([
    client.GET("/api/v1/orgs/{org_id}/costs/summary", { params: { path } }).then(must),
    client.GET("/api/v1/orgs/{org_id}/budgets-status", { params: { path } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/recommendations", { params: { path, query: { status: ["open"], limit: 5 } } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/anomalies", { params: { path, query: { status: "open" } } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/allocation/nodes", { params: { path, query: { limit: 500 } } }).then(maybe),
  ]);
  const cur = summary.currency ?? "EUR";
  const m = (v?: string) => formatMoney(v, cur, locale);
  const idx = nodeIndex(nodes?.items ?? [], t.common.unallocated);
  const change = summary.change_percent ?? "0";
  const up = compareDecimal(change, "0") > 0;
  const coverage = Number(summary.allocation_coverage_percent ?? 0);
  const daily = summary.daily ?? [];
  return (
    <>
      <PageHeader title={t.overview.title} description={`${summary.resource_count ?? 0} ${t.overview.resources}`} />
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Stat
          label={t.overview.monthToDate}
          value={m(summary.month_to_date)}
          sub={`${formatPercent(change, locale, 1, true)} ${t.overview.vsPrevious}`}
          tone={up ? "up" : "down"}
        />
        <Stat
          label={t.overview.forecast}
          value={m(summary.forecast_month_end)}
          sub={`${t.overview.forecastRange} : ${m(summary.forecast_lower)} – ${m(summary.forecast_upper)}`}
        />
        <Stat
          label={t.overview.savings}
          value={m(summary.potential_savings_monthly)}
          sub={`${summary.open_recommendations ?? 0} ${t.overview.savingsHint} · ${t.overview.realized} ${m(summary.realized_savings_monthly)}`}
        />
        <Card className="p-5">
          <p className="text-xs font-medium uppercase tracking-wide text-muted">{t.overview.coverage}</p>
          <p className="mt-2 text-2xl font-semibold tabular">{formatPercent(coverage, locale)}</p>
          <div className="mt-3">
            <Progress value={coverage} tone={coverage >= 95 ? "success" : "warning"} label={t.overview.coverage} />
          </div>
          <p className="mt-1 text-xs text-muted">{t.overview.coverageTarget}</p>
        </Card>
      </div>

      <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-3">
        <Card className="xl:col-span-2">
          <CardHeader title={t.overview.daily} action={<Link className="text-xs text-brand hover:underline" href={`/o/${org}/costs`}>{t.common.viewAll}</Link>} />
          <CardBody>
            {daily.length ? (
              <StackedBars
                label={t.overview.daily}
                categories={daily.map((d) => formatDay(d.period ?? "", locale))}
                series={[{ name: t.common.total, values: daily.map((d) => chartValue(d.amount)) }]}
                currency={cur}
                locale={locale}
              />
            ) : (
              <EmptyState title={t.common.noData} />
            )}
          </CardBody>
        </Card>
        <Card>
          <CardHeader title={t.overview.byTeam} />
          <CardBody>
            <Donut
              label={t.overview.byTeam}
              items={(summary.by_node ?? []).map((r) => ({ name: idx.name(r.keys?.allocation_node_id), value: chartValue(r.amount) }))}
              currency={cur}
              locale={locale}
            />
          </CardBody>
        </Card>
      </div>

      <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-3">
        <Card>
          <CardHeader title={t.overview.byType} />
          <CardBody className="space-y-2">
            {(summary.by_cost_type ?? []).map((r) => {
              const key = r.keys?.cost_type ?? "other";
              return (
                <div key={key} className="flex items-center justify-between text-sm">
                  <span className="text-muted">{(t.costTypes as Record<string, string>)[key] ?? key}</span>
                  <span className="tabular font-medium">{m(r.amount)}</span>
                </div>
              );
            })}
          </CardBody>
        </Card>
        <Card className="xl:col-span-2">
          <CardHeader title={t.overview.movers} />
          <Table>
            <thead>
              <tr>
                <Th>{t.common.name}</Th>
                <Th>{t.common.type}</Th>
                <Th align="right">7 j −1</Th>
                <Th align="right">7 j</Th>
                <Th align="right">Δ</Th>
              </tr>
            </thead>
            <tbody>
              {(summary.top_movers ?? []).map((mv) => (
                <tr key={mv.resource_id}>
                  <Td>
                    <Link className="hover:underline" href={`/o/${org}/resources/${mv.resource_id}`}>
                      {mv.name || mv.resource_id?.slice(0, 8)}
                    </Link>
                  </Td>
                  <Td className="text-muted">{resourceTypeLabel(mv.type ?? "", locale)}</Td>
                  <Td align="right">{m(mv.previous_7d)}</Td>
                  <Td align="right">{m(mv.current_7d)}</Td>
                  <Td align="right" className={compareDecimal(mv.delta ?? "0", "0") > 0 ? "text-danger" : "text-success"}>
                    {m(mv.delta)}
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      </div>

      <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-3">
        <Card>
          <CardHeader title={t.overview.anomalies} action={<Link className="text-xs text-brand hover:underline" href={`/o/${org}/anomalies`}>{t.common.viewAll}</Link>} />
          <ul className="divide-y divide-border">
            {(anomalies ?? []).slice(0, 5).map((a) => (
              <li key={a.id} className="px-5 py-3">
                <div className="flex items-center gap-2">
                  <Badge tone={a.severity === "critical" ? "danger" : a.severity === "warning" ? "warning" : "info"}>
                    {(t.anomalies.severity as Record<string, string>)[a.severity ?? "info"]}
                  </Badge>
                  <Link href={`/o/${org}/anomalies/${a.id}`} className="truncate text-sm font-medium hover:underline">
                    {a.title}
                  </Link>
                </div>
                <p className="mt-1 line-clamp-2 text-xs text-muted">{a.explanation}</p>
              </li>
            ))}
            {!anomalies?.length ? (
              <li>
                <EmptyState title={t.common.noData} />
              </li>
            ) : null}
          </ul>
        </Card>
        <Card>
          <CardHeader title={t.overview.topRecommendations} action={<Link className="text-xs text-brand hover:underline" href={`/o/${org}/recommendations`}>{t.common.viewAll}</Link>} />
          <ul className="divide-y divide-border">
            {(recos?.items ?? []).map((r) => (
              <li key={r.id} className="flex items-center gap-3 px-5 py-3">
                <div className="min-w-0 flex-1">
                  <Link href={`/o/${org}/recommendations/${r.id}`} className="block truncate text-sm font-medium hover:underline">
                    {r.title}
                  </Link>
                  <p className="text-xs text-muted">{(t.reco.types as Record<string, string>)[r.type ?? ""] ?? r.type}</p>
                </div>
                <span className="tabular text-sm font-semibold text-success">{m(r.savings_monthly)}</span>
              </li>
            ))}
            {!recos?.items?.length ? (
              <li>
                <EmptyState title={t.common.noData} />
              </li>
            ) : null}
          </ul>
        </Card>
        <Card>
          <CardHeader title={t.overview.budgets} action={<Link className="text-xs text-brand hover:underline" href={`/o/${org}/budgets`}>{t.common.viewAll}</Link>} />
          <CardBody className="space-y-4">
            {(budgets ?? []).map((b) => {
              const pct = Number(b.actual_percent ?? 0);
              const fpct = Number(b.forecast_percent ?? 0);
              return (
                <div key={b.budget?.id}>
                  <div className="flex items-baseline justify-between text-sm">
                    <span className="font-medium">{b.budget?.name}</span>
                    <span className="tabular text-xs text-muted">
                      {m(b.actual)} / {formatMoney(b.budget?.amount, cur, locale, 0)}
                    </span>
                  </div>
                  <div className="mt-1.5">
                    <Progress value={pct} tone={pct >= 100 ? "danger" : fpct > 100 ? "warning" : "brand"} label={b.budget?.name ?? ""} />
                  </div>
                  <p className="mt-1 text-xs text-muted">
                    {t.budgets.forecast} : {m(b.forecast)} ({formatPercent(fpct, locale, 0)})
                  </p>
                </div>
              );
            })}
            {!budgets?.length ? <EmptyState title={t.common.noData} /> : null}
          </CardBody>
        </Card>
      </div>
    </>
  );
}
