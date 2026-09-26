import { ApiButton } from "@/components/actions";
import { Badge, Card, CardBody, CardHeader, Progress, Table, Td, Th } from "@/components/ui/primitives";
import { api, must } from "@/lib/api/server";
import { formatDate, formatNumber } from "@/lib/format";
import { getI18n } from "@/lib/i18n";

const PLANS = [
  { id: "starter", name: "Starter", price: "~99 € / mois", target: "PME, 1 cloud" },
  { id: "team", name: "Team", price: "~1 % de la dépense suivie (min. 490 €)", target: "Multi-cloud + Kubernetes" },
  { id: "enterprise", name: "Enterprise", price: "Sur devis", target: "Self-hosted, SCIM, LLM souverain, SLA" },
  { id: "msp", name: "MSP", price: "Par client géré", target: "Infogéreurs, marque blanche" },
];

export default async function BillingPage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const b = must(await client.GET("/api/v1/orgs/{org_id}/billing", { params: { path: { org_id: org } } }));
  const l = b.limits;
  const lim = (v?: number) => (v === -1 ? "∞" : formatNumber(v ?? 0, locale, 0));
  const usage = [
    { label: "Utilisateurs", used: b.usage?.users ?? 0, max: l?.max_users ?? 0 },
    { label: "Connecteurs", used: b.usage?.connectors ?? 0, max: l?.max_connectors ?? 0 },
    { label: "Fournisseurs cloud", used: b.usage?.cloud_providers ?? 0, max: l?.max_cloud_providers ?? 0 },
    { label: "Tokens IA ce mois", used: b.usage?.llm_tokens_month ?? 0, max: l?.llm_tokens_month ?? 0 },
  ];
  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <Card>
        <CardHeader title={t.settings.plan} />
        <CardBody className="space-y-3">
          <p className="text-2xl font-semibold capitalize">{b.plan}</p>
          <p className="text-sm text-muted">
            {b.subscription?.status}
            {b.trial_ends_at ? ` · essai jusqu'au ${formatDate(b.trial_ends_at, locale)}` : ""}
          </p>
          <div className="flex flex-wrap gap-1">
            {Object.entries(l?.features ?? {})
              .filter(([, v]) => v)
              .map(([k]) => (
                <Badge key={k} tone="brand">
                  {k}
                </Badge>
              ))}
          </div>
          <ApiButton method="POST" path={`/api/v1/orgs/${org}/billing/portal`} variant="secondary" size="sm" followUrl>
            {t.settings.portal}
          </ApiButton>
        </CardBody>
      </Card>
      <Card className="xl:col-span-2">
        <CardHeader title={t.settings.limits} />
        <CardBody className="space-y-4">
          {usage.map((u) => (
            <div key={u.label}>
              <div className="flex justify-between text-sm">
                <span>{u.label}</span>
                <span className="tabular text-muted">
                  {formatNumber(u.used, locale, 0)} / {lim(u.max)}
                </span>
              </div>
              <div className="mt-1">
                <Progress value={u.max > 0 ? (u.used / u.max) * 100 : 0} tone={u.max > 0 && u.used >= u.max ? "danger" : "brand"} label={u.label} />
              </div>
            </div>
          ))}
        </CardBody>
      </Card>
      <Card className="xl:col-span-3">
        <CardHeader title={t.settings.upgrade} description="Les limites sont appliquées par l'API." />
        <Table>
          <thead>
            <tr>
              <Th>Plan</Th>
              <Th>Cible</Th>
              <Th>Prix indicatif</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {PLANS.map((p) => (
              <tr key={p.id}>
                <Td className="font-medium">
                  {p.name} {p.id === b.plan ? <Badge tone="success">actuel</Badge> : null}
                </Td>
                <Td className="text-muted">{p.target}</Td>
                <Td>{p.price}</Td>
                <Td align="right">
                  {p.id !== b.plan && (p.id === "starter" || p.id === "team") ? (
                    <ApiButton method="POST" path={`/api/v1/orgs/${org}/billing/checkout`} body={{ plan: p.id }} size="sm" followUrl>
                      Choisir
                    </ApiButton>
                  ) : p.id !== b.plan ? (
                    <a className="text-sm text-brand hover:underline" href="mailto:sales@kairn.io">
                      Nous contacter
                    </a>
                  ) : null}
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      </Card>
    </div>
  );
}
