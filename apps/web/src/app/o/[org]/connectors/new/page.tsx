import { PageHeader } from "@/components/ui/primitives";
import { api, must } from "@/lib/api/server";
import { getI18n } from "@/lib/i18n";

import { ConnectorWizard } from "./wizard";

export default async function NewConnectorPage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { t } = await getI18n();
  const client = await api();
  const types = must(await client.GET("/api/v1/connector-types")) ?? [];
  return (
    <>
      <PageHeader title={t.connectors.add} description={t.connectors.readOnly} />
      <ConnectorWizard
        org={org}
        types={types.map((ty) => ({
          type: ty.type ?? "",
          name: ty.display_name ?? "",
          category: ty.category ?? "",
          provider: ty.provider ?? "",
          resources: ty.resources ?? [],
          fields: (ty.fields ?? []).map((f) => ({ name: f.name ?? "", label: f.label ?? "", secret: !!f.secret, required: !!f.required, help: f.help ?? "", def: f.default ?? "" })),
          permissions: (ty.permissions ?? []).map((p) => ({ scope: p.scope ?? "", description: p.description ?? "", optional: !!p.optional })),
          interval: ty.default_interval_seconds ?? 3600,
          docs: ty.docs_url ?? "",
        }))}
        labels={{
          choose: t.connectors.choose,
          configure: t.connectors.configure,
          permissions: t.connectors.permissions,
          resources: t.connectors.resources,
          validate: t.connectors.validate,
          create: t.common.create,
          name: t.common.name,
          backfill: t.connectors.backfill,
          back: t.common.back,
          guide: t.connectors.guide,
          categories: t.connectors.categories as Record<string, string>,
        }}
      />
    </>
  );
}
