import { api, must } from "@/lib/api/server";
import { getI18n } from "@/lib/i18n";

import { OrgForm } from "./org-form";

export default async function OrgSettingsPage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { t } = await getI18n();
  const client = await api();
  const o = must(await client.GET("/api/v1/orgs/{org_id}", { params: { path: { org_id: org } } }));
  const s = o.settings ?? {};
  return (
    <OrgForm
      org={org}
      initial={{
        name: o.name ?? "",
        currency: o.currency ?? "EUR",
        locale: o.locale ?? "fr",
        timezone: o.timezone ?? "Europe/Paris",
        vat: o.vat_rate ?? "",
        k8s: s.k8s_allocation_method || "max",
        idle: s.k8s_idle_mode || "keep",
        preferInvoice: !!s.prefer_invoice,
        llm: s.llm_provider || "anthropic",
        external: !!s.allow_external_llm,
        recipients: (s.report_recipients ?? []).join(", "),
        sso: s.sso_idp_alias ?? "",
        ssoEnforced: !!s.sso_enforced,
        percentile: s.rightsizing_percentile || 95,
        window: s.rightsizing_window_days || 14,
      }}
      settingsRaw={s as Record<string, unknown>}
      labels={{
        org: t.settings.organization,
        name: t.common.name,
        currency: t.settings.currency,
        timezone: t.settings.timezone,
        vat: t.settings.vat,
        language: t.common.language,
        k8s: t.settings.k8sMethod,
        idle: t.settings.idleMode,
        preferInvoice: t.settings.preferInvoice,
        ai: t.settings.ai,
        llm: t.settings.llmProvider,
        external: t.settings.allowExternal,
        recipients: t.settings.reportRecipients,
        security: t.settings.security,
        sso: t.settings.ssoAlias,
        save: t.common.save,
      }}
    />
  );
}
