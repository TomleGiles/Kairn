import { notFound } from "next/navigation";

import { api, must } from "@/lib/api/server";
import { getI18n } from "@/lib/i18n";

import { Shell } from "./shell";

export default async function OrgLayout({ children, params }: { children: React.ReactNode; params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const me = must(await client.GET("/api/v1/me"));
  const orgRes = await client.GET("/api/v1/orgs/{org_id}", { params: { path: { org_id: org } } });
  if (orgRes.response.status === 404) notFound();
  const current = must(orgRes);
  const memberships = (me.memberships ?? []).map((m) => ({ id: m.org_id ?? "", name: m.org_name ?? "", plan: m.plan ?? "" }));
  if (!memberships.some((m) => m.id === org)) memberships.push({ id: org, name: current.name ?? "", plan: current.plan ?? "" });
  return (
    <Shell
      org={{ id: org, name: current.name ?? "", plan: current.plan ?? "", parent: current.parent_org_id ?? null }}
      orgs={memberships}
      user={{ name: me.user?.name ?? "", email: me.user?.email ?? "" }}
      locale={locale}
      t={{ nav: t.nav, common: t.common }}
    >
      {children}
    </Shell>
  );
}
