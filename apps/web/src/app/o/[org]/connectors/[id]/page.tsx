import Link from "next/link";

import { ApiButton, CopyButton } from "@/components/actions";
import { Badge, Card, CardBody, CardHeader, Code, PageHeader, Table, Td, Th } from "@/components/ui/primitives";
import { api, must } from "@/lib/api/server";
import { formatDate, relativeTime } from "@/lib/format";
import { getI18n } from "@/lib/i18n";

export default async function ConnectorPage({ params }: { params: Promise<{ org: string; id: string }> }) {
  const { org, id } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const path = { org_id: org, id };
  const [c, runs] = await Promise.all([
    client.GET("/api/v1/orgs/{org_id}/connectors/{id}", { params: { path } }).then(must),
    client.GET("/api/v1/orgs/{org_id}/connectors/{id}/runs", { params: { path } }).then(must),
  ]);
  const statuses = t.connectors.statuses as Record<string, string>;
  const tone = c.status === "ok" ? "success" : c.status === "degraded" ? "warning" : c.status === "error" ? "danger" : "neutral";
  return (
    <>
      <PageHeader
        title={c.name ?? ""}
        description={c.type}
        actions={
          <>
            <ApiButton method="POST" path={`/api/v1/orgs/${org}/connectors/${id}/test`} variant="secondary">
              {t.connectors.test}
            </ApiButton>
            <ApiButton method="POST" path={`/api/v1/orgs/${org}/connectors/${id}/sync`} body={{}}>
              {t.connectors.sync}
            </ApiButton>
            <Link className="text-sm text-brand hover:underline" href={`/o/${org}/connectors`}>
              ← {t.common.back}
            </Link>
          </>
        }
      />
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <Card>
          <CardBody className="space-y-3 text-sm">
            <p>
              <Badge tone={tone}>{statuses[c.status ?? "pending"]}</Badge>
            </p>
            {c.status_message ? <p className="text-muted">{c.status_message}</p> : null}
            <p>
              {t.connectors.lastSync} : {relativeTime(c.last_sync_at, locale)}
            </p>
            <p>
              {t.connectors.interval} : {Math.round((c.interval_seconds ?? 0) / 60)} min
            </p>
            <div>
              <p className="text-xs font-medium uppercase text-muted">Paramètres</p>
              <dl className="mt-1 grid grid-cols-2 gap-x-2 gap-y-1">
                {Object.entries(c.settings ?? {}).map(([k, v]) => (
                  <div key={k} className="contents">
                    <dt className="text-muted">{k}</dt>
                    <dd className="truncate font-mono text-xs leading-5">{v}</dd>
                  </div>
                ))}
              </dl>
            </div>
            {c.secret_keys?.length ? (
              <p className="text-xs text-muted">
                Secrets : {c.secret_keys.join(", ")} <span className="text-subtle">(chiffrés, jamais réaffichés)</span>
              </p>
            ) : null}
            {c.webhook_url ? (
              <div>
                <p className="text-xs font-medium uppercase text-muted">{t.connectors.webhook}</p>
                <Code className="mt-1">{c.webhook_url}</Code>
                <div className="mt-2 flex gap-2">
                  <CopyButton value={c.webhook_url} label={t.common.copy} copied={t.common.copied} />
                  <ApiButton method="POST" path={`/api/v1/orgs/${org}/connectors/${id}/webhook/rotate`} size="sm" variant="secondary" confirm="Régénérer l'URL ? L'ancienne cessera de fonctionner.">
                    Régénérer
                  </ApiButton>
                </div>
              </div>
            ) : null}
            <ApiButton method="DELETE" path={`/api/v1/orgs/${org}/connectors/${id}`} variant="danger" size="sm" confirm="Supprimer ce connecteur ?">
              {t.common.delete}
            </ApiButton>
          </CardBody>
        </Card>
        <Card className="xl:col-span-2">
          <CardHeader title={t.connectors.runs} />
          <Table>
            <thead>
              <tr>
                <Th>Début</Th>
                <Th>Volet</Th>
                <Th>{t.common.status}</Th>
                <Th align="right">Éléments</Th>
                <Th>Erreur</Th>
              </tr>
            </thead>
            <tbody>
              {(runs ?? []).slice(0, 50).map((r) => (
                <tr key={r.id}>
                  <Td className="whitespace-nowrap">{formatDate(r.started_at, locale, true)}</Td>
                  <Td>{r.kind}</Td>
                  <Td>
                    <Badge tone={r.status === "ok" ? "success" : r.status === "error" ? "danger" : "neutral"}>{r.status}</Badge>
                  </Td>
                  <Td align="right">{r.items}</Td>
                  <Td className="max-w-sm truncate text-xs text-danger">{r.error}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      </div>
    </>
  );
}
