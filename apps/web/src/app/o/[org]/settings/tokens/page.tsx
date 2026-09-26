import { ApiButton } from "@/components/actions";
import { Badge, Card, CardBody, EmptyState, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe } from "@/lib/api/server";
import { formatDate, relativeTime } from "@/lib/format";
import { getI18n } from "@/lib/i18n";

import { TokenForm } from "./token-form";

export default async function TokensPage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const tokens = await client.GET("/api/v1/orgs/{org_id}/tokens", { params: { path: { org_id: org } } }).then(maybe);
  const roles = t.settings.roles as Record<string, string>;
  return (
    <Card>
      <Table>
        <thead>
          <tr>
            <Th>{t.common.name}</Th>
            <Th>Préfixe</Th>
            <Th>{t.settings.role}</Th>
            <Th>Scopes</Th>
            <Th>Dernière utilisation</Th>
            <Th>Expiration</Th>
            <Th />
          </tr>
        </thead>
        <tbody>
          {(tokens ?? []).map((tk) => (
            <tr key={tk.id} className={tk.revoked_at ? "opacity-50" : ""}>
              <Td className="font-medium">{tk.name}</Td>
              <Td className="font-mono text-xs">kairn_{tk.prefix}_…</Td>
              <Td>{roles[tk.role ?? "viewer"]}</Td>
              <Td className="text-xs text-muted">{tk.scopes?.length ? tk.scopes.join(", ") : "—"}</Td>
              <Td className="text-muted">{relativeTime(tk.last_used_at, locale)}</Td>
              <Td className="text-muted">{tk.expires_at ? formatDate(tk.expires_at, locale) : "—"}</Td>
              <Td align="right">
                {tk.revoked_at ? (
                  <Badge>révoqué</Badge>
                ) : (
                  <ApiButton method="DELETE" path={`/api/v1/orgs/${org}/tokens/${tk.id}`} size="sm" variant="ghost" confirm="Révoquer ce jeton ?">
                    Révoquer
                  </ApiButton>
                )}
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>
      {!tokens?.length ? <EmptyState title={t.common.noData} /> : null}
      <CardBody className="border-t border-border">
        <TokenForm org={org} roles={roles} labels={{ name: t.common.name, role: t.settings.role, create: t.settings.newToken, once: t.settings.tokenOnce, copy: t.common.copy, copied: t.common.copied }} />
      </CardBody>
    </Card>
  );
}
