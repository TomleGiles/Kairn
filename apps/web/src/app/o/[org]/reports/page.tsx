import { ApiButton } from "@/components/actions";
import { Badge, Card, EmptyState, PageHeader, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe } from "@/lib/api/server";
import { formatDate } from "@/lib/format";
import { getI18n } from "@/lib/i18n";

export default async function ReportsPage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const reports = await client.GET("/api/v1/orgs/{org_id}/reports", { params: { path: { org_id: org }, query: {} } }).then(maybe);
  const now = new Date();
  const prev = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - 1, 1)).toISOString().slice(0, 7);
  const current = now.toISOString().slice(0, 7);
  const statuses = t.reports.statuses as Record<string, string>;
  return (
    <>
      <PageHeader
        title={t.reports.title}
        description={t.reports.subtitle}
        actions={
          reports ? (
            <>
              <ApiButton method="POST" path={`/api/v1/orgs/${org}/reports/generate`} body={{ period: prev }} variant="secondary">
                {t.reports.generate} {prev}
              </ApiButton>
              <ApiButton method="POST" path={`/api/v1/orgs/${org}/reports/generate`} body={{ period: current }}>
                {t.reports.generate} {current}
              </ApiButton>
            </>
          ) : null
        }
      />
      <Card>
        <Table>
          <thead>
            <tr>
              <Th>{t.reports.period}</Th>
              <Th>{t.common.status}</Th>
              <Th>Synthèse</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {(reports?.items ?? []).map((r) => (
              <tr key={r.id}>
                <Td className="font-medium">{r.period}</Td>
                <Td>
                  <Badge tone={r.status === "ready" || r.status === "sent" ? "success" : r.status === "error" ? "danger" : "neutral"}>{statuses[r.status ?? "pending"] ?? r.status}</Badge>
                  {r.sent_at ? <span className="ml-2 text-xs text-muted">{formatDate(r.sent_at, locale, true)}</span> : null}
                </Td>
                <Td className="max-w-xl text-sm text-muted">{String(r.summary?.["headline"] ?? "")}</Td>
                <Td align="right">
                  {r.object_key ? (
                    <a className="text-sm text-brand hover:underline" href={`/api/v1/orgs/${org}/reports/${r.id}/download`}>
                      {t.reports.download}
                    </a>
                  ) : null}
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
        {!reports?.items?.length ? <EmptyState title={t.common.noData} description={reports ? undefined : "Plan Team requis."} /> : null}
      </Card>
    </>
  );
}
