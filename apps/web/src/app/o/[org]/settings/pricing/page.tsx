import { Badge, Card, CardBody, CardHeader, EmptyState, Table, Td, Th } from "@/components/ui/primitives";
import { api, maybe } from "@/lib/api/server";
import { formatDate, formatMoney } from "@/lib/format";
import { getI18n } from "@/lib/i18n";

import { AdjustmentForm } from "./adjustment-form";

export default async function PricingPage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const path = { org_id: org };
  const [cats, onprem, adjs] = await Promise.all([
    client.GET("/api/v1/orgs/{org_id}/pricing/catalogs", { params: { path } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/pricing/onprem-models", { params: { path, query: {} } }).then(maybe),
    client.GET("/api/v1/orgs/{org_id}/pricing/adjustments", { params: { path, query: {} } }).then(maybe),
  ]);
  return (
    <div className="space-y-4">
      <Card>
        <CardHeader title={t.settings.catalogs} description="Grilles versionnées : une grille négociée propre à l'organisation prime sur la grille publique." />
        <Table>
          <thead>
            <tr>
              <Th>{t.common.provider}</Th>
              <Th>Version</Th>
              <Th>{t.common.source}</Th>
              <Th>Valide depuis</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {(cats ?? []).map((c) => (
              <tr key={c.id}>
                <Td className="font-medium">{c.provider}</Td>
                <Td className="font-mono text-xs">{c.version}</Td>
                <Td>
                  <Badge tone={c.org_id ? "brand" : "neutral"}>{c.org_id ? "négociée" : c.source}</Badge>
                </Td>
                <Td className="text-muted">{formatDate(c.valid_from, locale)}</Td>
                <Td className="text-xs text-muted">{c.currency}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      </Card>
      <Card>
        <CardHeader title={t.settings.onprem} description="Amortissement matériel, électricité (PUE), licences, main-d'œuvre → coût par vCPU, Go de RAM et Go de stockage." />
        {(onprem?.items ?? []).length ? (
          <Table>
            <thead>
              <tr>
                <Th>{t.common.name}</Th>
                <Th align="right">Matériel</Th>
                <Th align="right">Amortissement</Th>
                <Th align="right">Capacité</Th>
              </tr>
            </thead>
            <tbody>
              {(onprem?.items ?? []).map((m) => (
                <tr key={m.id}>
                  <Td>{m.name}</Td>
                  <Td align="right">{formatMoney(m.hardware_cost, m.currency, locale, 0)}</Td>
                  <Td align="right">{m.amortization_months} mois</Td>
                  <Td align="right">
                    {m.capacity_vcpu} vCPU · {m.capacity_ram_gb} Go
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : (
          <EmptyState title={t.common.noData} description="Créez un modèle via l'API ou Terraform (kairn_onprem_model)." />
        )}
      </Card>
      <Card>
        <CardHeader title={t.settings.adjustments} />
        <Table>
          <thead>
            <tr>
              <Th>{t.common.name}</Th>
              <Th>{t.common.type}</Th>
              <Th>Périmètre</Th>
              <Th align="right">Valeur</Th>
              <Th>Validité</Th>
            </tr>
          </thead>
          <tbody>
            {(adjs?.items ?? []).map((a) => (
              <tr key={a.id}>
                <Td>{a.name}</Td>
                <Td>
                  <Badge>{a.kind}</Badge>
                </Td>
                <Td className="font-mono text-xs">
                  {a.provider || "*"} / {a.sku_pattern || "*"}
                </Td>
                <Td align="right">{a.percent ? `${a.percent} %` : formatMoney(a.amount ?? "0", a.currency, locale)}</Td>
                <Td className="text-muted">
                  {formatDate(a.valid_from, locale)} → {a.valid_to ? formatDate(a.valid_to, locale) : "∞"}
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
        <CardBody className="border-t border-border">
          <AdjustmentForm org={org} />
        </CardBody>
      </Card>
    </div>
  );
}
