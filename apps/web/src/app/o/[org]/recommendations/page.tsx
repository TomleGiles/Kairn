import Link from "next/link";

import { Badge, Card, EmptyState, PageHeader, Stat, Table, Td, Th } from "@/components/ui/primitives";
import { api, must } from "@/lib/api/server";
import { formatMoney } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { riskTone } from "@/lib/utils";

export default async function RecommendationsPage({ params, searchParams }: { params: Promise<{ org: string }>; searchParams: Promise<{ status?: string; type?: string }> }) {
  const { org } = await params;
  const sp = await searchParams;
  const { locale, t } = await getI18n();
  const client = await api();
  const path = { org_id: org };
  const status = sp.status ?? "open";
  const [summary, page] = await Promise.all([
    client.GET("/api/v1/orgs/{org_id}/recommendations/summary", { params: { path } }).then(must),
    client.GET("/api/v1/orgs/{org_id}/recommendations", { params: { path, query: { status: [status], type: sp.type ? [sp.type] : undefined, limit: 200 } } }).then(must),
  ]);
  const cur = summary.currency ?? "EUR";
  const statuses = ["open", "accepted", "postponed", "applied", "dismissed"];
  const types = t.reco.types as Record<string, string>;
  return (
    <>
      <PageHeader title={t.reco.title} description={t.reco.subtitle} />
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Stat label={t.reco.potential} value={formatMoney(summary.potential_monthly, cur, locale)} sub={`${summary.open_count ?? 0} ${t.reco.statuses.open.toLowerCase()}s`} />
        <Stat label={t.reco.accepted} value={formatMoney(summary.accepted_monthly, cur, locale)} />
        <Stat label={t.reco.realized} value={formatMoney(summary.realized_monthly, cur, locale)} />
      </div>
      <nav aria-label={t.common.status} className="mt-6 flex flex-wrap gap-1 text-sm">
        {statuses.map((s) => (
          <Link key={s} href={`?status=${s}`} aria-current={s === status ? "page" : undefined} className={`rounded-md px-3 py-1.5 ${s === status ? "bg-brand-soft text-brand" : "text-muted hover:bg-surface-2"}`}>
            {(t.reco.statuses as Record<string, string>)[s]}
          </Link>
        ))}
      </nav>
      <Card className="mt-3">
        <Table>
          <thead>
            <tr>
              <Th>{t.common.name}</Th>
              <Th>{t.common.type}</Th>
              <Th>{t.reco.risk}</Th>
              <Th align="right">{t.reco.savings}</Th>
            </tr>
          </thead>
          <tbody>
            {(page.items ?? []).map((r) => (
              <tr key={r.id}>
                <Td>
                  <Link className="font-medium hover:underline" href={`/o/${org}/recommendations/${r.id}`}>
                    {r.title}
                  </Link>
                  <p className="line-clamp-1 max-w-2xl text-xs text-muted">{r.summary}</p>
                </Td>
                <Td className="whitespace-nowrap text-muted">{types[r.type ?? ""] ?? r.type}</Td>
                <Td>
                  <Badge tone={riskTone(r.risk)}>{(t.reco.risks as Record<string, string>)[r.risk ?? "low"]}</Badge>
                </Td>
                <Td align="right" className="font-semibold text-success">
                  {formatMoney(r.savings_monthly, r.currency, locale)}
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
        {!page.items?.length ? <EmptyState title={t.common.noData} /> : null}
      </Card>
    </>
  );
}
