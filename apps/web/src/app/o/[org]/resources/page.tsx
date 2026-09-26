import Link from "next/link";

import { Badge, Card, CardHeader, EmptyState, Input, PageHeader, Select, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe, must } from "@/lib/api/server";
import { formatMoney } from "@/lib/format";
import { getI18n } from "@/lib/i18n";
import { resourceTypeLabel } from "@/lib/utils";

const TYPES = ["compute.instance", "storage.volume", "storage.snapshot", "storage.bucket", "network.ip", "network.loadbalancer", "project", "k8s.cluster", "k8s.node", "k8s.namespace", "k8s.workload", "k8s.pod", "host"];

export default async function ResourcesPage({
  params,
  searchParams,
}: {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ q?: string; type?: string; cursor?: string }>;
}) {
  const { org } = await params;
  const sp = await searchParams;
  const { locale, t } = await getI18n();
  const client = await api();
  const path = { org_id: org };
  const [page, orphans] = await Promise.all([
    client
      .GET("/api/v1/orgs/{org_id}/resources", { params: { path, query: { q: sp.q, type: sp.type ? [sp.type] : undefined, cursor: sp.cursor, limit: 100 } } })
      .then(must),
    client.GET("/api/v1/orgs/{org_id}/orphans", { params: { path } }).then(maybe),
  ]);
  const items = page.items ?? [];
  const next = new URLSearchParams({ ...(sp.q ? { q: sp.q } : {}), ...(sp.type ? { type: sp.type } : {}), cursor: page.next_cursor ?? "" });
  return (
    <>
      <PageHeader title={t.inventory.title} description={t.inventory.subtitle} />
      <form className="mb-4 flex flex-wrap items-end gap-3" role="search">
        <div className="w-72">
          <label htmlFor="q" className="mb-1 block text-xs font-medium text-muted">
            {t.common.search}
          </label>
          <Input id="q" name="q" defaultValue={sp.q} placeholder="web-1, vol-…" />
        </div>
        <div>
          <label htmlFor="type" className="mb-1 block text-xs font-medium text-muted">
            {t.common.type}
          </label>
          <Select id="type" name="type" defaultValue={sp.type ?? ""}>
            <option value="">{t.common.all}</option>
            {TYPES.map((ty) => (
              <option key={ty} value={ty}>
                {resourceTypeLabel(ty, locale)}
              </option>
            ))}
          </Select>
        </div>
        <button className="h-9 rounded-md border border-border bg-surface px-4 text-sm hover:bg-surface-2" type="submit">
          {t.common.filter}
        </button>
      </form>
      <Card>
        <Table>
          <thead>
            <tr>
              <Th>{t.common.name}</Th>
              <Th>{t.common.type}</Th>
              <Th>{t.common.provider}</Th>
              <Th>{t.common.region}</Th>
              <Th>{t.inventory.labels}</Th>
              <Th align="right">{t.common.cost30d}</Th>
            </tr>
          </thead>
          <tbody>
            {items.map((r) => (
              <tr key={r.id} className="hover:bg-surface-2/50">
                <Td>
                  <Link className="font-medium hover:underline" href={`/o/${org}/resources/${r.id}`}>
                    {r.name || r.external_id}
                  </Link>
                  {r.attributes && typeof r.attributes["flavor"] === "string" ? <span className="ml-2 text-xs text-subtle">{String(r.attributes["flavor"])}</span> : null}
                </Td>
                <Td>{resourceTypeLabel(r.type ?? "", locale)}</Td>
                <Td className="text-muted">{r.provider}</Td>
                <Td className="text-muted">{r.region}</Td>
                <Td>
                  <div className="flex flex-wrap gap-1">
                    {Object.entries(r.labels ?? {})
                      .slice(0, 3)
                      .map(([k, v]) => (
                        <Badge key={k}>
                          {k}={v}
                        </Badge>
                      ))}
                  </div>
                </Td>
                <Td align="right">{formatMoney(r.cost_30d, r.currency, locale)}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
        {!items.length ? <EmptyState title={t.common.noData} /> : null}
        {page.next_cursor ? (
          <div className="border-t border-border p-3 text-right">
            <Link className="text-sm text-brand hover:underline" href={`?${next}`}>
              {t.common.next} →
            </Link>
          </div>
        ) : null}
      </Card>
      {orphans?.length ? (
        <Card className="mt-6">
          <CardHeader title={t.inventory.orphans} description={t.inventory.orphansHint} />
          <Table>
            <thead>
              <tr>
                <Th>{t.common.name}</Th>
                <Th>{t.inventory.reason}</Th>
                <Th align="right">{t.inventory.monthlyEstimate}</Th>
              </tr>
            </thead>
            <tbody>
              {orphans.map((o) => (
                <tr key={o.resource?.id}>
                  <Td>
                    <Link className="hover:underline" href={`/o/${org}/resources/${o.resource?.id}`}>
                      {o.resource?.name}
                    </Link>
                  </Td>
                  <Td className="text-muted">{o.reason}</Td>
                  <Td align="right">{formatMoney(o.monthly_cost_estimate, o.currency, locale)}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      ) : null}
    </>
  );
}
