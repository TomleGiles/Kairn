import { ApiButton } from "@/components/actions";
import { Badge, Card, CardBody, CardHeader, EmptyState, PageHeader, Progress, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe } from "@/lib/api/server";
import { formatDate, formatMoney, formatPercent } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { nodeIndex } from "@/lib/nodes";
import { severityTone } from "@/lib/utils";

import { BudgetForm, ChannelForm } from "./forms";

export default async function BudgetsPage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const path = { org_id: org };
  const [statuses, alerts, channels, rules, silences, nodes, orgInfo] = await Promise.all([
    client.GET("/api/v1/orgs/{org_id}/budgets-status", { params: { path } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/alerts", { params: { path, query: { limit: 50 } } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/channels", { params: { path, query: {} } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/alert-rules", { params: { path, query: {} } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/silences", { params: { path, query: {} } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/allocation/nodes", { params: { path, query: { limit: 500 } } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}", { params: { path } }).then(maybe),
  ]);
  const idx = nodeIndex(nodes?.items ?? [], t.common.unallocated);
  const periods: Record<string, string> = { monthly: t.budgets.monthly, quarterly: t.budgets.quarterly, yearly: t.budgets.yearly };
  const channelName = new Map((channels?.items ?? []).map((c) => [c.id ?? "", c.name ?? ""]));
  return (
    <>
      <PageHeader title={t.budgets.title} description={t.budgets.subtitle} />
      <Card>
        <CardHeader title={t.budgets.budget} />
        <Table>
          <thead>
            <tr>
              <Th>{t.common.name}</Th>
              <Th>{t.budgets.scope}</Th>
              <Th>{t.budgets.period}</Th>
              <Th align="right">{t.budgets.amount}</Th>
              <Th align="right">{t.budgets.actual}</Th>
              <Th className="w-48" />
              <Th align="right">{t.budgets.forecast}</Th>
            </tr>
          </thead>
          <tbody>
            {(statuses ?? []).map((s) => {
              const b = s.budget;
              const pct = Number(s.actual_percent ?? 0);
              const fpct = Number(s.forecast_percent ?? 0);
              return (
                <tr key={b?.id}>
                  <Td className="font-medium">{b?.name}</Td>
                  <Td className="text-muted">{b?.node_id ? idx.path(b.node_id) : t.budgets.wholeOrg}</Td>
                  <Td>{periods[b?.period ?? "monthly"]}</Td>
                  <Td align="right">{formatMoney(b?.amount, b?.currency, locale, 0)}</Td>
                  <Td align="right">{formatMoney(s.actual, s.currency, locale)}</Td>
                  <Td>
                    <div className="flex items-center gap-2">
                      <Progress value={pct} tone={pct >= 100 ? "danger" : fpct > 100 ? "warning" : "brand"} label={b?.name ?? ""} />
                      <span className="w-12 text-right text-xs tabular">{formatPercent(pct, locale, 0)}</span>
                    </div>
                  </Td>
                  <Td align="right" className={fpct > 100 ? "text-warning" : ""}>
                    {formatMoney(s.forecast, s.currency, locale)} <span className="text-xs text-muted">({formatPercent(fpct, locale, 0)})</span>
                  </Td>
                </tr>
              );
            })}
          </tbody>
        </Table>
        {!statuses?.length ? <EmptyState title={t.common.noData} /> : null}
        <div className="border-t border-border p-5">
          <BudgetForm
            org={org}
            currency={orgInfo?.currency ?? "EUR"}
            nodes={(nodes?.items ?? []).map((n) => ({ id: n.id ?? "", label: idx.path(n.id) }))}
            channels={(channels?.items ?? []).map((c) => ({ id: c.id ?? "", label: c.name ?? "" }))}
            labels={{ title: t.budgets.newBudget, name: t.common.name, amount: t.budgets.amount, period: t.budgets.period, scope: t.budgets.scope, whole: t.budgets.wholeOrg, create: t.common.create, periods, channels: t.budgets.channels }}
          />
        </div>
      </Card>

      <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader title={t.budgets.alerts} />
          <ul className="divide-y divide-border">
            {(alerts?.items ?? []).map((a) => (
              <li key={a.id} className="px-5 py-3">
                <div className="flex items-center gap-2">
                  <Badge tone={severityTone(a.severity)}>{a.severity}</Badge>
                  <span className="text-sm font-medium">{a.title}</span>
                </div>
                <p className="mt-1 text-xs text-muted">
                  {a.body} — {formatDate(a.last_at, locale, true)} · {t.budgets.count} : {a.count} · {a.status}
                  {a.notified_at ? " · ✉" : ""}
                </p>
              </li>
            ))}
          </ul>
          {!alerts?.items?.length ? <EmptyState title={t.common.noData} /> : null}
        </Card>
        <Card>
          <CardHeader title={t.budgets.channels} />
          <ul className="divide-y divide-border">
            {(channels?.items ?? []).map((c) => (
              <li key={c.id} className="flex items-center justify-between gap-3 px-5 py-3 text-sm">
                <span>
                  <Badge>{c.kind}</Badge> <span className="font-medium">{c.name}</span>
                </span>
                <ApiButton method="POST" path={`/api/v1/orgs/${org}/channels/${c.id}/test`} size="sm" variant="secondary">
                  {t.budgets.testChannel}
                </ApiButton>
              </li>
            ))}
          </ul>
          <CardBody className="border-t border-border">
            <ChannelForm org={org} labels={{ name: t.common.name, create: t.common.create }} />
          </CardBody>
          <CardHeader title={t.budgets.rules} className="border-t" />
          <ul className="divide-y divide-border">
            {(rules?.items ?? []).map((r) => (
              <li key={r.id} className="px-5 py-3 text-sm">
                <span className="font-medium">{r.name}</span> <Badge>{r.kind}</Badge>
                <p className="text-xs text-muted">
                  {Object.entries(r.config ?? {})
                    .map(([k, v]) => `${k}=${v}`)
                    .join(", ")}{" "}
                  → {(r.channel_ids ?? []).map((id) => channelName.get(id) ?? id).join(", ") || "—"}
                </p>
              </li>
            ))}
          </ul>
          {silences?.items?.length ? (
            <>
              <CardHeader title={t.budgets.silences} className="border-t" />
              <ul className="divide-y divide-border">
                {silences.items.map((s) => (
                  <li key={s.id} className="px-5 py-3 text-sm">
                    {Object.entries(s.matchers ?? {})
                      .map(([k, v]) => `${k}=${v}`)
                      .join(", ")}{" "}
                    — {formatDate(s.starts_at, locale, true)} → {formatDate(s.ends_at, locale, true)} ({s.reason})
                  </li>
                ))}
              </ul>
            </>
          ) : null}
        </Card>
      </div>
    </>
  );
}
