import Link from "next/link";

import { Badge, Card, CardBody, CardHeader, EmptyState, PageHeader, Progress, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe, must, type Schemas } from "@/lib/api/server";
import { formatMoney, formatPercent } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { nodeIndex } from "@/lib/nodes";
import { resourceTypeLabel } from "@/lib/utils";

import { NodeForm, RuleForm } from "./forms";

function condLabel(c: Schemas["Condition"]): string {
  const v = c.op === "in" ? (c.values ?? []).join(", ") : c.value;
  return `${c.field} ${c.op}${v ? ` ${v}` : ""}`;
}

export default async function AllocationPage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const path = { org_id: org };
  const now = new Date();
  const lastMonth = `${now.getUTCMonth() === 0 ? now.getUTCFullYear() - 1 : now.getUTCFullYear()}-${String(now.getUTCMonth() === 0 ? 12 : now.getUTCMonth()).padStart(2, "0")}`;
  const [coverage, nodes, rules, shared] = await Promise.all([
    client.GET("/api/v1/orgs/{org_id}/allocation/coverage", { params: { path, query: {} } }).then(must),
    client.GET("/api/v1/orgs/{org_id}/allocation/nodes", { params: { path, query: { limit: 500 } } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/allocation/rules", { params: { path, query: { limit: 500 } } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/allocation/shared-rules", { params: { path, query: { limit: 500 } } }).then(maybe),
  ]);
  const cur = coverage.currency ?? "EUR";
  const idx = nodeIndex(nodes?.items ?? [], t.common.unallocated);
  const cov = Number(coverage.coverage_percent ?? 0);
  const nodeList = (nodes?.items ?? []).map((n) => ({ id: n.id ?? "", label: idx.path(n.id), kind: n.kind ?? "" }));
  // Arbre : profondeur de chaque nœud pour l'indentation.
  const depth = (n: Schemas["AllocationNode"]) => ((n.path ?? "").split("/").filter(Boolean).length || 1) - 1;
  const ordered = [...(nodes?.items ?? [])].sort((a, b) => idx.path(a.id).localeCompare(idx.path(b.id)));
  return (
    <>
      <PageHeader
        title={t.allocation.title}
        description={t.allocation.subtitle}
        actions={
          <a className="text-sm text-brand hover:underline" href={`/api/v1/orgs/${org}/chargeback?month=${lastMonth}&format=csv`}>
            {t.allocation.chargeback} {lastMonth} (CSV)
          </a>
        }
      />
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <Card>
          <CardHeader title={t.allocation.coverage} description="30 j" />
          <CardBody>
            <p className="text-3xl font-semibold tabular">{formatPercent(cov, locale)}</p>
            <div className="mt-3">
              <Progress value={cov} tone={cov >= 95 ? "success" : "warning"} label={t.allocation.coverage} />
            </div>
            <dl className="mt-4 space-y-1 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted">{t.common.total}</dt>
                <dd className="tabular">{formatMoney(coverage.total, cur, locale)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted">{t.common.unallocated}</dt>
                <dd className="tabular">{formatMoney(coverage.unallocated, cur, locale)}</dd>
              </div>
            </dl>
            {coverage.top_unallocated?.length ? (
              <>
                <h3 className="mb-2 mt-4 text-xs font-medium uppercase text-muted">{t.allocation.topUnallocated}</h3>
                <ul className="space-y-1 text-sm">
                  {coverage.top_unallocated.map((m) => (
                    <li key={m.resource_id} className="flex justify-between gap-2">
                      <Link className="truncate hover:underline" href={`/o/${org}/resources/${m.resource_id}`}>
                        {m.name || m.resource_id}
                      </Link>
                      <span className="tabular">{formatMoney(m.current_7d, cur, locale)}</span>
                    </li>
                  ))}
                </ul>
              </>
            ) : null}
          </CardBody>
        </Card>
        <Card className="xl:col-span-2">
          <CardHeader title={`${t.allocation.chargeback} — 30 j`} />
          <Table>
            <thead>
              <tr>
                <Th>{t.allocation.node}</Th>
                <Th align="right">{t.allocation.direct}</Th>
                <Th align="right">{t.allocation.sharedPart}</Th>
                <Th align="right">{t.common.total}</Th>
              </tr>
            </thead>
            <tbody>
              {(coverage.by_node ?? []).map((r) => (
                <tr key={r.node_id}>
                  <Td>{r.node_id === "unallocated" ? <Badge tone="warning">{t.common.unallocated}</Badge> : r.path}</Td>
                  <Td align="right">{formatMoney(r.direct, cur, locale)}</Td>
                  <Td align="right" className="text-muted">
                    {formatMoney(r.shared, cur, locale)}
                  </Td>
                  <Td align="right" className="font-medium">
                    {formatMoney(r.total, cur, locale)}
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      </div>

      <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-3">
        <Card>
          <CardHeader title={t.allocation.tree} />
          <CardBody>
            <ul className="space-y-1 text-sm" aria-label={t.allocation.tree}>
              {ordered.map((n) => (
                <li key={n.id} style={{ paddingLeft: depth(n) * 16 }} className="flex items-center gap-2">
                  <span>{n.name}</span>
                  <Badge>{n.kind}</Badge>
                </li>
              ))}
            </ul>
            <div className="mt-4 border-t border-border pt-4">
              <NodeForm org={org} nodes={nodeList} labels={{ title: t.allocation.newNode, name: t.common.name, create: t.common.create, parent: t.allocation.node }} />
            </div>
          </CardBody>
        </Card>
        <Card className="xl:col-span-2">
          <CardHeader title={t.allocation.rules} description="La première règle correspondante (priorité croissante) attribue la ressource. Les labels des parents sont hérités." />
          <Table>
            <thead>
              <tr>
                <Th align="right">{t.allocation.priority}</Th>
                <Th>{t.common.name}</Th>
                <Th>{t.allocation.conditions}</Th>
                <Th>{t.allocation.node}</Th>
              </tr>
            </thead>
            <tbody>
              {[...(rules?.items ?? [])]
                .sort((a, b) => (a.priority ?? 0) - (b.priority ?? 0))
                .map((r) => (
                  <tr key={r.id}>
                    <Td align="right">{r.priority}</Td>
                    <Td>
                      {r.name} {!r.enabled ? <Badge>off</Badge> : null}
                    </Td>
                    <Td className="font-mono text-xs">{(r.conditions ?? []).map(condLabel).join(" ∧ ")}</Td>
                    <Td>{idx.name(r.node_id)}</Td>
                  </tr>
                ))}
            </tbody>
          </Table>
          {!rules?.items?.length ? <EmptyState title={t.common.noData} /> : null}
          <div className="border-t border-border p-5">
            <RuleForm
              org={org}
              nodes={nodeList}
              labels={{ title: t.allocation.newRule, name: t.common.name, priority: t.allocation.priority, node: t.allocation.node, preview: t.allocation.preview, matched: t.allocation.matched, create: t.common.create }}
            />
          </div>
        </Card>
      </div>
      {shared?.items?.length ? (
        <Card className="mt-4">
          <CardHeader title={t.allocation.shared} />
          <Table>
            <thead>
              <tr>
                <Th>{t.common.name}</Th>
                <Th>{t.allocation.conditions}</Th>
                <Th>{t.allocation.method}</Th>
                <Th>{t.allocation.targets}</Th>
              </tr>
            </thead>
            <tbody>
              {shared.items.map((r) => (
                <tr key={r.id}>
                  <Td>{r.name}</Td>
                  <Td className="font-mono text-xs">{(r.source ?? []).map(condLabel).join(" ∧ ")}</Td>
                  <Td>{r.method}</Td>
                  <Td>{(r.targets ?? []).map((x) => idx.name(x.node_id) + (r.method === "proportional" ? "" : ` (${x.weight})`)).join(", ")}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      ) : null}
      <p className="mt-4 text-xs text-subtle">{resourceTypeLabel("k8s.pod", locale)} : label.team, attr.k8s.namespace, attr.k8s.workload…</p>
    </>
  );
}
