import { TopologyGraph } from "@/components/charts/charts";
import { Card, CardBody, EmptyState, PageHeader } from "@/components/ui/primitives";
import { api, must } from "@/lib/api/server";
import { chartValue } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { resourceTypeLabel } from "@/lib/utils";

export default async function TopologyPage({ params, searchParams }: { params: Promise<{ org: string }>; searchParams: Promise<{ root?: string; depth?: string }> }) {
  const { org } = await params;
  const sp = await searchParams;
  const { locale, t } = await getI18n();
  const client = await api();
  const depth = Math.min(5, Math.max(1, Number(sp.depth ?? 3)));
  const topo = must(
    await client.GET("/api/v1/orgs/{org_id}/topology", { params: { path: { org_id: org }, query: { root: sp.root, depth } } }),
  );
  const nodes = (topo.nodes ?? []).map((n) => ({ id: n.id ?? "", name: n.name ?? "", type: resourceTypeLabel(n.type ?? "", locale), cost: chartValue(n.cost_30d) }));
  const edges = (topo.edges ?? []).map((e) => ({ source: e.parent_id ?? "", target: e.child_id ?? "", relation: e.relation ?? "" }));
  return (
    <>
      <PageHeader
        title={t.topology.title}
        description={t.topology.subtitle}
        actions={
          <nav aria-label="Profondeur" className="flex gap-1 text-sm">
            {[1, 2, 3, 4].map((d) => (
              <a key={d} href={`?depth=${d}${sp.root ? `&root=${sp.root}` : ""}`} aria-current={d === depth ? "page" : undefined} className={`rounded-md px-2.5 py-1 ${d === depth ? "bg-brand-soft text-brand" : "text-muted hover:bg-surface-2"}`}>
                {d}
              </a>
            ))}
          </nav>
        }
      />
      <Card>
        <CardBody>{nodes.length ? <TopologyGraph label={t.topology.title} nodes={nodes} edges={edges} currency="EUR" locale={locale} /> : <EmptyState title={t.common.noData} />}</CardBody>
      </Card>
    </>
  );
}
