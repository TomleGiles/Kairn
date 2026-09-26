import Link from "next/link";

import { Badge, Card, CardHeader, EmptyState, PageHeader, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe } from "@/lib/api/server";
import { formatDate, formatNumber, formatPercent } from "@/lib/format";
import { getI18n } from "@/lib/i18n";

import { CheckForm } from "./check-form";
import { UptimeBars } from "./uptime-bars";

export default async function UptimePage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const path = { org_id: org };
  const [summary, pages, incidents] = await Promise.all([
    client.GET("/api/v1/orgs/{org_id}/uptime/summary", { params: { path } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/status-pages", { params: { path, query: {} } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/incidents", { params: { path, query: {} } }).then(maybe),
  ]);
  if (!summary) {
    return (
      <>
        <PageHeader title={t.uptime.title} description={t.uptime.subtitle} />
        <EmptyState title="Plan Team requis" />
      </>
    );
  }
  return (
    <>
      <PageHeader title={t.uptime.title} description={t.uptime.subtitle} />
      <Card>
        <CardHeader title={t.uptime.checks} />
        <Table>
          <thead>
            <tr>
              <Th>{t.common.name}</Th>
              <Th>{t.common.status}</Th>
              <Th align="right">{t.uptime.latency}</Th>
              <Th align="right">{t.uptime.uptime24h}</Th>
              <Th align="right">{t.uptime.uptime30d}</Th>
              <Th className="w-72">30 j</Th>
            </tr>
          </thead>
          <tbody>
            {summary.map((c) => (
              <tr key={c.check_id}>
                <Td>
                  <span className="font-medium">{c.name}</span> <Badge>{c.kind}</Badge>
                </Td>
                <Td>
                  {c.up === undefined || c.up === null ? (
                    <Badge>{t.uptime.unknown}</Badge>
                  ) : c.up ? (
                    <Badge tone="success">{t.uptime.up}</Badge>
                  ) : (
                    <Badge tone="danger">{t.uptime.down}</Badge>
                  )}
                </Td>
                <Td align="right">{formatNumber(c.latency_ms, locale, 0)} ms</Td>
                <Td align="right">{formatPercent(c.uptime_24h, locale, 2)}</Td>
                <Td align="right">{formatPercent(c.uptime_30d, locale, 3)}</Td>
                <Td>
                  <UptimeBars days={(c.daily ?? []).map((d) => ({ day: d.day ?? "", uptime: d.uptime ?? 100 }))} />
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
        {!summary.length ? <EmptyState title={t.common.noData} /> : null}
        <div className="border-t border-border p-5">
          <CheckForm org={org} labels={{ title: t.uptime.newCheck, name: t.common.name, target: t.uptime.target, create: t.common.create }} />
        </div>
      </Card>
      <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader title={t.uptime.statusPages} />
          <ul className="divide-y divide-border">
            {(pages?.items ?? []).map((p) => (
              <li key={p.id} className="flex items-center justify-between px-5 py-3 text-sm">
                <span>
                  <span className="font-medium">{p.title}</span> <Badge>{p.public ? "public" : "privée"}</Badge>
                </span>
                <Link className="text-brand hover:underline" href={`/status/${p.slug}`} target="_blank">
                  /status/{p.slug} ↗
                </Link>
              </li>
            ))}
          </ul>
          {!pages?.items?.length ? <EmptyState title={t.common.noData} /> : null}
        </Card>
        <Card>
          <CardHeader title={t.uptime.incidents} />
          <ul className="divide-y divide-border">
            {(incidents?.items ?? []).map((i) => (
              <li key={i.id} className="px-5 py-3 text-sm">
                <div className="flex items-center gap-2">
                  <Badge tone={i.status === "open" ? "danger" : "success"}>{i.status}</Badge>
                  <span className="font-medium">{i.title}</span>
                </div>
                <p className="mt-1 text-xs text-muted">
                  {t.uptime.started} {formatDate(i.started_at, locale, true)}
                  {i.resolved_at ? ` · ${t.uptime.resolved} ${formatDate(i.resolved_at, locale, true)}` : ""} · {i.source}
                </p>
              </li>
            ))}
          </ul>
          {!incidents?.items?.length ? <EmptyState title={t.common.noData} /> : null}
        </Card>
      </div>
    </>
  );
}
