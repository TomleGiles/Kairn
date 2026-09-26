import Link from "next/link";

import { Badge, buttonVariants, Card, EmptyState, PageHeader, Table, Td, Th } from "@/components/ui/primitives";
import { api, must } from "@/lib/api/server";
import { relativeTime } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { statusTone } from "@/lib/utils";

export default async function ConnectorsPage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const [conns, types] = await Promise.all([
    client.GET("/api/v1/orgs/{org_id}/connectors", { params: { path: { org_id: org } } }).then(must),
    client.GET("/api/v1/connector-types").then(must),
  ]);
  const typeName = new Map((types ?? []).map((ty) => [ty.type ?? "", ty.display_name ?? ""]));
  const statuses = t.connectors.statuses as Record<string, string>;
  return (
    <>
      <PageHeader
        title={t.connectors.title}
        description={`${t.connectors.subtitle} ${t.connectors.readOnly}`}
        actions={
          <Link className={buttonVariants()} href={`/o/${org}/connectors/new`}>
            {t.connectors.add}
          </Link>
        }
      />
      <Card>
        <Table>
          <thead>
            <tr>
              <Th>{t.common.name}</Th>
              <Th>{t.common.type}</Th>
              <Th>{t.connectors.health}</Th>
              <Th>{t.connectors.lastSync}</Th>
              <Th align="right">{t.connectors.interval}</Th>
            </tr>
          </thead>
          <tbody>
            {(conns ?? []).map((c) => (
              <tr key={c.id}>
                <Td>
                  <Link className="font-medium hover:underline" href={`/o/${org}/connectors/${c.id}`}>
                    {c.name}
                  </Link>
                </Td>
                <Td className="text-muted">{typeName.get(c.type ?? "") ?? c.type}</Td>
                <Td>
                  <Badge tone={statusTone(c.status)}>{statuses[c.status ?? "pending"] ?? c.status}</Badge>
                  {c.status_message && c.status !== "ok" ? <p className="mt-1 max-w-md truncate text-xs text-muted">{c.status_message}</p> : null}
                </Td>
                <Td className="text-muted">{relativeTime(c.last_sync_at, locale)}</Td>
                <Td align="right">{Math.round((c.interval_seconds ?? 0) / 60)} min</Td>
              </tr>
            ))}
          </tbody>
        </Table>
        {!conns?.length ? <EmptyState title={t.common.noData} action={<Link className={buttonVariants()} href={`/o/${org}/connectors/new`}>{t.connectors.add}</Link>} /> : null}
      </Card>
    </>
  );
}
