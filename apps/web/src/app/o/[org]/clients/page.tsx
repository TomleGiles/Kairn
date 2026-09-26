import Link from "next/link";

import { Card, EmptyState, PageHeader, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe } from "@/lib/api/server";
import { formatDate } from "@/lib/format";
import { getI18n } from "@/lib/i18n";

import { ClientForm } from "./client-form";

export default async function ClientsPage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const clients = await client.GET("/api/v1/orgs/{org_id}/clients", { params: { path: { org_id: org } } }).then(maybe);
  if (clients === null) return <EmptyState title="Le mode MSP nécessite le plan MSP." />;
  return (
    <>
      <PageHeader title={t.nav.clients} description="Organisations clientes gérées, rapports en marque blanche et chargeback par client." />
      <Card>
        <Table>
          <thead>
            <tr>
              <Th>{t.common.name}</Th>
              <Th>Plan</Th>
              <Th>Créée le</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {clients.map((c) => (
              <tr key={c.id}>
                <Td className="font-medium">{c.name}</Td>
                <Td>{c.plan}</Td>
                <Td className="text-muted">{formatDate(c.created_at, locale)}</Td>
                <Td align="right">
                  <Link className="text-sm text-brand hover:underline" href={`/o/${c.id}`}>
                    {t.common.open} →
                  </Link>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
        {!clients.length ? <EmptyState title={t.common.noData} /> : null}
        <div className="border-t border-border p-5">
          <ClientForm org={org} />
        </div>
      </Card>
    </>
  );
}
