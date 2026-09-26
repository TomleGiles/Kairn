import { ForecastChart } from "@/components/charts/charts";
import { Badge, Card, CardBody, CardHeader, PageHeader, Stat } from "@/components/ui/primitives";
import { api, maybe, must } from "@/lib/api/server";
import { chartValue, formatDay, formatMoney } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { nodeIndex } from "@/lib/nodes";

import { WhatIf } from "./whatif";

export default async function ForecastPage({ params, searchParams }: { params: Promise<{ org: string }>; searchParams: Promise<{ node?: string }> }) {
  const { org } = await params;
  const sp = await searchParams;
  const { locale, t } = await getI18n();
  const client = await api();
  const path = { org_id: org };
  const [fc, nodes, flavors] = await Promise.all([
    client.GET("/api/v1/orgs/{org_id}/forecast", { params: { path, query: { node_id: sp.node } } }).then(must),
    client.GET("/api/v1/orgs/{org_id}/allocation/nodes", { params: { path, query: { limit: 500 } } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/resources", { params: { path, query: { type: ["compute.instance"], limit: 500 } } }).then(maybe),
  ]);
  const f = fc.forecast!;
  const cur = f.currency ?? "EUR";
  const idx = nodeIndex(nodes?.items ?? [], t.common.unallocated);
  const history = (fc.history ?? []).slice(-45).map((h) => ({ day: formatDay(h.period ?? "", locale), value: chartValue(h.amount) }));
  const points = (f.points ?? []).map((p) => ({ day: formatDay(p.day ?? "", locale), value: chartValue(p.value), lower: chartValue(p.lower), upper: chartValue(p.upper) }));
  const vms = (flavors?.items ?? []).map((r) => ({ id: r.id ?? "", name: r.name ?? "", provider: r.provider ?? "", flavor: String(r.attributes?.["flavor"] ?? "") }));
  return (
    <>
      <PageHeader title={t.forecast.title} description={t.forecast.subtitle} />
      <form className="mb-4 flex items-end gap-2">
        <div>
          <label htmlFor="node" className="mb-1 block text-xs font-medium text-muted">
            {t.budgets.scope}
          </label>
          <select id="node" name="node" defaultValue={sp.node ?? ""} className="h-9 rounded-md border border-border bg-surface px-2 text-sm">
            <option value="">{t.budgets.wholeOrg}</option>
            {(nodes?.items ?? []).map((n) => (
              <option key={n.id} value={n.id}>
                {idx.path(n.id)}
              </option>
            ))}
          </select>
        </div>
        <button className="h-9 rounded-md border border-border bg-surface px-4 text-sm hover:bg-surface-2">{t.common.apply}</button>
      </form>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Stat label={t.overview.forecast} value={formatMoney(f.total, cur, locale)} sub={`${t.forecast.interval} : ${formatMoney(f.lower, cur, locale)} – ${formatMoney(f.upper, cur, locale)}`} />
        <Card className="p-5">
          <p className="text-xs font-medium uppercase tracking-wide text-muted">{t.forecast.model}</p>
          <p className="mt-2 font-mono text-sm">{f.model}</p>
          <Badge className="mt-2" tone={fc.source === "analytics" ? "success" : "neutral"}>
            {fc.source}
          </Badge>
        </Card>
        <Stat label={t.budgets.period} value={formatDay(f.period_end ?? "", locale)} />
      </div>
      <Card className="mt-4">
        <CardBody>
          <ForecastChart
            label={t.forecast.title}
            history={history}
            forecast={points}
            currency={cur}
            locale={locale}
            labels={{ history: t.forecast.history, forecast: t.forecast.prediction, interval: t.forecast.interval }}
          />
        </CardBody>
      </Card>
      <Card className="mt-4">
        <CardHeader title={t.forecast.whatif} />
        <CardBody>
          <WhatIf
            org={org}
            vms={vms}
            locale={locale}
            labels={{
              addNodes: t.forecast.addNodes,
              changeFlavor: t.forecast.changeFlavor,
              migrate: t.forecast.migrate,
              simulate: t.forecast.simulate,
              current: t.forecast.current,
              projected: t.forecast.projected,
              delta: t.forecast.delta,
              assumptions: t.forecast.assumptions,
              unmapped: t.forecast.unmapped,
              count: t.forecast.count,
              flavor: t.forecast.flavor,
              target: t.forecast.target,
              provider: t.common.provider,
            }}
          />
        </CardBody>
      </Card>
    </>
  );
}
