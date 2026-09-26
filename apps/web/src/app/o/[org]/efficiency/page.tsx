import Link from "next/link";

import { StackedBars } from "@/components/charts/charts";
import { Card, CardBody, CardHeader, EmptyState, PageHeader, Progress, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe, must } from "@/lib/api/server";
import { chartValue, formatDay, formatMoney, formatNumber, formatPercent } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { resourceTypeLabel } from "@/lib/utils";

export default async function EfficiencyPage({ params, searchParams }: { params: Promise<{ org: string }>; searchParams: Promise<{ level?: string }> }) {
  const { org } = await params;
  const sp = await searchParams;
  const { locale, t } = await getI18n();
  const level = ["resource", "workload", "node"].includes(sp.level ?? "") ? (sp.level as "resource" | "workload" | "node") : "resource";
  const client = await api();
  const path = { org_id: org };
  const [rowsRaw, units] = await Promise.all([
    client.GET("/api/v1/orgs/{org_id}/efficiency", { params: { path, query: { level } } }).then(must),
    client.GET("/api/v1/orgs/{org_id}/unit-metrics", { params: { path, query: {} } }).then(maybe),
  ]);
  const rows = rowsRaw ?? [];
  const unitCosts = await Promise.all(
    (units?.items ?? []).map((u) => client.GET("/api/v1/orgs/{org_id}/unit-metrics/{id}/costs", { params: { path: { org_id: org, id: u.id ?? "" } } }).then(maybe)),
  );
  const levels = [
    { key: "resource", label: t.efficiency.resource },
    { key: "workload", label: t.efficiency.workload },
    { key: "node", label: t.efficiency.node },
  ];
  return (
    <>
      <PageHeader
        title={t.efficiency.title}
        description={t.efficiency.subtitle}
        actions={
          <nav aria-label={t.efficiency.level} className="flex gap-1 text-sm">
            {levels.map((l) => (
              <Link key={l.key} href={`?level=${l.key}`} aria-current={l.key === level ? "page" : undefined} className={`rounded-md px-3 py-1.5 ${l.key === level ? "bg-brand-soft text-brand" : "text-muted hover:bg-surface-2"}`}>
                {l.label}
              </Link>
            ))}
          </nav>
        }
      />
      <Card>
        <Table>
          <thead>
            <tr>
              <Th>{t.common.name}</Th>
              <Th>{t.common.type}</Th>
              <Th align="right">{t.common.cost30d}</Th>
              {level === "workload" ? (
                <>
                  <Th align="right">{t.efficiency.requested}</Th>
                  <Th align="right">{t.efficiency.used}</Th>
                </>
              ) : (
                <>
                  <Th align="right">{t.efficiency.cpuAvg}</Th>
                  {level === "resource" ? <Th align="right">{t.efficiency.cpuP95}</Th> : null}
                  {level === "resource" ? <Th align="right">{t.efficiency.memAvg}</Th> : null}
                </>
              )}
              <Th className="w-40">{t.efficiency.efficiency}</Th>
              <Th align="right">{t.efficiency.waste}</Th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => {
              const eff = (r.efficiency ?? 0) * 100;
              return (
                <tr key={r.key}>
                  <Td className="font-medium">
                    {level === "resource" ? (
                      <Link className="hover:underline" href={`/o/${org}/resources/${r.key}`}>
                        {r.name}
                      </Link>
                    ) : (
                      r.name || r.key
                    )}
                  </Td>
                  <Td className="text-muted">{resourceTypeLabel(r.kind ?? "", locale)}</Td>
                  <Td align="right">{formatMoney(r.cost_30d, r.currency, locale)}</Td>
                  {level === "workload" ? (
                    <>
                      <Td align="right">{formatNumber(r.requested_cpu, locale)}</Td>
                      <Td align="right">{formatNumber(r.used_cpu, locale)}</Td>
                    </>
                  ) : (
                    <>
                      <Td align="right">{formatPercent((r.cpu_avg ?? 0) * 100, locale)}</Td>
                      {level === "resource" ? <Td align="right">{formatPercent((r.cpu_p95 ?? 0) * 100, locale)}</Td> : null}
                      {level === "resource" ? <Td align="right">{formatPercent((r.mem_avg ?? 0) * 100, locale)}</Td> : null}
                    </>
                  )}
                  <Td>
                    <div className="flex items-center gap-2">
                      <Progress value={eff} tone={eff < 20 ? "danger" : eff < 45 ? "warning" : "success"} label={`${t.efficiency.efficiency} ${r.name}`} />
                      <span className="w-12 text-right text-xs tabular text-muted">{formatPercent(eff, locale, 0)}</span>
                    </div>
                  </Td>
                  <Td align="right" className="text-danger">
                    {formatMoney(r.waste_estimate_30d, r.currency, locale)}
                  </Td>
                </tr>
              );
            })}
          </tbody>
        </Table>
        {!rows.length ? <EmptyState title={t.common.noData} /> : null}
      </Card>
      {unitCosts.filter(Boolean).map((u) =>
        u ? (
          <Card key={u.metric?.id} className="mt-4">
            <CardHeader
              title={`${t.efficiency.unitCosts} — ${u.metric?.name}`}
              description={`${t.efficiency.costPerUnit} (${u.metric?.unit_label}) : ${formatMoney(u.average_cost_per_unit, u.currency, locale, 4)}`}
            />
            <CardBody>
              <StackedBars
                label={u.metric?.name ?? ""}
                categories={(u.points ?? []).map((p) => formatDay(p.day ?? "", locale))}
                series={[{ name: t.efficiency.costPerUnit, values: (u.points ?? []).map((p) => Number(p.cost_per_unit ?? 0)) }]}
                currency={u.currency ?? "EUR"}
                locale={locale}
                height={200}
              />
            </CardBody>
          </Card>
        ) : null,
      )}
    </>
  );
}
