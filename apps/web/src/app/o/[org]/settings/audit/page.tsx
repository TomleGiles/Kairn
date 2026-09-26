import Link from "next/link";

import { Card, EmptyState, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe } from "@/lib/api/server";
import { formatDate } from "@/lib/format";
import { getI18n } from "@/lib/i18n";

export default async function AuditPage({ params, searchParams }: { params: Promise<{ org: string }>; searchParams: Promise<{ cursor?: string; action?: string }> }) {
  const { org } = await params;
  const sp = await searchParams;
  const { locale, t } = await getI18n();
  const client = await api();
  const page = await client.GET("/api/v1/orgs/{org_id}/audit", { params: { path: { org_id: org }, query: { cursor: sp.cursor, action: sp.action, limit: 100 } } }).then(maybe);
  if (!page) return <EmptyState title="Accès réservé aux propriétaires et administrateurs." />;
  return (
    <Card>
      <div className="flex justify-end gap-3 border-b border-border px-5 py-3 text-sm">
        <a className="text-brand hover:underline" href={`/api/v1/orgs/${org}/audit/export?format=csv`}>
          CSV
        </a>
        <a className="text-brand hover:underline" href={`/api/v1/orgs/${org}/audit/export?format=json`}>
          JSON
        </a>
      </div>
      <Table>
        <thead>
          <tr>
            <Th>Date</Th>
            <Th>Acteur</Th>
            <Th>Action</Th>
            <Th>Cible</Th>
            <Th>IP</Th>
          </tr>
        </thead>
        <tbody>
          {(page.items ?? []).map((e) => (
            <tr key={e.id}>
              <Td className="whitespace-nowrap">{formatDate(e.at, locale, true)}</Td>
              <Td className="font-mono text-xs">
                {e.actor_type}:{(e.actor_id ?? "").slice(0, 8)}
              </Td>
              <Td className="font-mono text-xs">{e.action}</Td>
              <Td className="font-mono text-xs text-muted">
                {e.target_type} {(e.target_id ?? "").slice(0, 8)}
              </Td>
              <Td className="text-xs text-muted">{e.ip}</Td>
            </tr>
          ))}
        </tbody>
      </Table>
      {!page.items?.length ? <EmptyState title={t.common.noData} /> : null}
      {page.next_cursor ? (
        <div className="border-t border-border p-3 text-right">
          <Link className="text-sm text-brand hover:underline" href={`?cursor=${page.next_cursor}`}>
            {t.common.next} →
          </Link>
        </div>
      ) : null}
    </Card>
  );
}
