import Link from "next/link";

import { CopyButton } from "@/components/actions";
import { Badge, Card, CardBody, CardHeader, Code, PageHeader } from "@/components/ui/primitives";
import { api, must } from "@/lib/api/server";
import { formatDate, formatMoney } from "@/lib/format";
import { getI18n } from "@/lib/i18n";

import { RecoActions } from "./reco-actions";

export default async function RecommendationPage({ params }: { params: Promise<{ org: string; id: string }> }) {
  const { org, id } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const r = must(await client.GET("/api/v1/orgs/{org_id}/recommendations/{id}", { params: { path: { org_id: org, id } } }));
  const types = t.reco.types as Record<string, string>;
  const risk = r.risk ?? "low";
  const rem = r.remediation;
  return (
    <>
      <PageHeader
        title={r.title ?? ""}
        description={types[r.type ?? ""] ?? r.type}
        actions={<Link className="text-sm text-brand hover:underline" href={`/o/${org}/recommendations`}>← {t.common.back}</Link>}
      />
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <Card className="xl:col-span-2">
          <CardBody className="space-y-4">
            <p className="text-sm leading-relaxed">{r.summary}</p>
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone={risk === "high" ? "danger" : risk === "medium" ? "warning" : "success"}>
                {t.reco.risk} : {(t.reco.risks as Record<string, string>)[risk]}
              </Badge>
              <Badge tone="brand">{(t.reco.statuses as Record<string, string>)[r.status ?? "open"]}</Badge>
              {r.status_reason ? <span className="text-xs text-muted">« {r.status_reason} »</span> : null}
              {r.postponed_until ? <span className="text-xs text-muted">→ {formatDate(r.postponed_until, locale)}</span> : null}
            </div>
            <RecoActions
              org={org}
              id={id}
              status={r.status ?? "open"}
              labels={{ accept: t.reco.accept, postpone: t.reco.postpone, dismiss: t.reco.dismiss, applied: t.reco.markApplied, reopen: t.reco.reopen, reason: t.reco.reason, until: t.reco.until, cancel: t.common.cancel }}
            />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <p className="text-xs font-medium uppercase tracking-wide text-muted">{t.reco.savings}</p>
            <p className="mt-2 text-3xl font-semibold tabular text-success">{formatMoney(r.savings_monthly, r.currency, locale)}</p>
            {r.measured_savings_monthly ? (
              <p className="mt-2 text-sm">
                {t.reco.measured} : <span className="font-semibold tabular">{formatMoney(r.measured_savings_monthly, r.currency, locale)}</span>
              </p>
            ) : null}
            <p className="mt-3 text-sm">
              <Link className="text-brand hover:underline" href={`/o/${org}/resources/${r.resource_id}`}>
                {t.common.details} →
              </Link>
            </p>
          </CardBody>
        </Card>
      </div>
      <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader title={t.reco.howTo} />
          <CardBody className="space-y-4">
            <ol className="list-decimal space-y-1 pl-5 text-sm">
              {(rem?.steps ?? []).map((s) => (
                <li key={s}>{s}</li>
              ))}
            </ol>
            {[
              { label: "CLI", value: rem?.cli },
              { label: "Kubernetes", value: rem?.manifest },
              { label: "Terraform / OpenTofu", value: rem?.terraform },
            ]
              .filter((x) => x.value)
              .map((x) => (
                <div key={x.label}>
                  <div className="mb-1 flex items-center justify-between">
                    <span className="text-xs font-medium uppercase text-muted">{x.label}</span>
                    <CopyButton value={x.value ?? ""} label={t.common.copy} copied={t.common.copied} />
                  </div>
                  <Code>{x.value}</Code>
                </div>
              ))}
            <p className="text-xs text-subtle">{t.connectors.readOnly}</p>
          </CardBody>
        </Card>
        <Card>
          <CardHeader title={t.reco.evidence} />
          <CardBody>
            <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
              {Object.entries(r.evidence ?? {}).map(([k, v]) => (
                <div key={k} className="contents">
                  <dt className="text-muted">{k}</dt>
                  <dd className="font-mono text-xs leading-5">{typeof v === "object" ? JSON.stringify(v) : String(v)}</dd>
                </div>
              ))}
            </dl>
          </CardBody>
        </Card>
      </div>
    </>
  );
}
