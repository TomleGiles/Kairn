import Link from "next/link";

import { Badge, Card, EmptyState, PageHeader, Table, Td, Th } from "@/components/ui/primitives";
import { api, must } from "@/lib/api/server";
import { formatDate, formatMoney } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { severityTone } from "@/lib/utils";

export default async function AnomaliesPage({ params, searchParams }: { params: Promise<{ org: string }>; searchParams: Promise<{ status?: string }> }) {
  const { org } = await params;
  const sp = await searchParams;
  const { locale, t } = await getI18n();
  const client = await api();
  const status = (["open", "acknowledged", "resolved"].includes(sp.status ?? "") ? sp.status : undefined) as "open" | "acknowledged" | "resolved" | undefined;
  const list = must(await client.GET("/api/v1/orgs/{org_id}/anomalies", { params: { path: { org_id: org }, query: { status } } })) ?? [];
  return (
    <>
      <PageHeader title={t.anomalies.title} description={t.anomalies.subtitle} />
      <nav aria-label={t.common.status} className="mb-3 flex gap-1 text-sm">
        {[undefined, "open", "acknowledged", "resolved"].map((s) => (
          <Link key={s ?? "all"} href={s ? `?status=${s}` : "?"} aria-current={s === status ? "page" : undefined} className={`rounded-md px-3 py-1.5 ${s === status ? "bg-brand-soft text-brand" : "text-muted hover:bg-surface-2"}`}>
            {s ? (t.anomalies.statuses as Record<string, string>)[s] : t.common.all}
          </Link>
        ))}
      </nav>
      <Card>
        <Table>
          <thead>
            <tr>
              <Th>{t.common.name}</Th>
              <Th>{t.anomalies.window}</Th>
              <Th align="right">{t.anomalies.expected}</Th>
              <Th align="right">{t.anomalies.actual}</Th>
              <Th>{t.common.status}</Th>
            </tr>
          </thead>
          <tbody>
            {list.map((a) => (
              <tr key={a.id}>
                <Td>
                  <div className="flex items-center gap-2">
                    <Badge tone={severityTone(a.severity)}>{(t.anomalies.severity as Record<string, string>)[a.severity ?? "info"]}</Badge>
                    <Link className="font-medium hover:underline" href={`/o/${org}/anomalies/${a.id}`}>
                      {a.title}
                    </Link>
                  </div>
                  <p className="mt-1 line-clamp-2 max-w-3xl text-xs text-muted">{a.explanation}</p>
                </Td>
                <Td className="whitespace-nowrap text-muted">{formatDate(a.window_start, locale)}</Td>
                <Td align="right">{formatMoney(a.expected, a.currency, locale)}</Td>
                <Td align="right" className="font-semibold">
                  {formatMoney(a.actual, a.currency, locale)}
                </Td>
                <Td>{(t.anomalies.statuses as Record<string, string>)[a.status ?? "open"]}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
        {!list.length ? <EmptyState title={t.common.noData} /> : null}
      </Card>
    </>
  );
}
