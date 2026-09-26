import { ApiButton } from "@/components/actions";
import { Card, CardBody, Table, Td, Th } from "@/components/ui/primitives";
import { api, must } from "@/lib/api/server";
import { formatDate } from "@/lib/format";
import { getI18n } from "@/lib/i18n";

import { InviteForm, RoleSelect } from "./forms";

export default async function MembersPage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const members = must(await client.GET("/api/v1/orgs/{org_id}/members", { params: { path: { org_id: org } } })) ?? [];
  const roles = t.settings.roles as Record<string, string>;
  return (
    <Card>
      <Table>
        <thead>
          <tr>
            <Th>{t.common.name}</Th>
            <Th>{t.settings.email}</Th>
            <Th>{t.settings.role}</Th>
            <Th>Depuis</Th>
            <Th />
          </tr>
        </thead>
        <tbody>
          {members.map((m) => (
            <tr key={m.user_id}>
              <Td className="font-medium">{m.name}</Td>
              <Td className="text-muted">{m.email}</Td>
              <Td>
                <RoleSelect org={org} userId={m.user_id ?? ""} role={m.role ?? "viewer"} roles={roles} />
              </Td>
              <Td className="text-muted">{formatDate(m.created_at, locale)}</Td>
              <Td align="right">
                <ApiButton method="DELETE" path={`/api/v1/orgs/${org}/members/${m.user_id}`} size="sm" variant="ghost" confirm={`Retirer ${m.email} ?`}>
                  {t.common.delete}
                </ApiButton>
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>
      <CardBody className="border-t border-border">
        <InviteForm org={org} roles={roles} labels={{ email: t.settings.email, invite: t.settings.invite, role: t.settings.role }} />
      </CardBody>
    </Card>
  );
}
