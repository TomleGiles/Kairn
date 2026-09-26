"use client";

import { useState } from "react";

import { ErrorText, useApi } from "@/components/actions";
import { Button, Card, CardBody, CardHeader, Input, Select } from "@/components/ui/primitives";

type State = {
  name: string;
  currency: string;
  locale: string;
  timezone: string;
  vat: string;
  k8s: string;
  idle: string;
  preferInvoice: boolean;
  llm: string;
  external: boolean;
  recipients: string;
  sso: string;
  ssoEnforced: boolean;
  percentile: number;
  window: number;
};

// Défini hors du composant : une définition interne remonterait les champs à chaque frappe.
function Row({ id, label, children }: { id: string; label: string; children: React.ReactNode }) {
  return (
    <div>
      <label htmlFor={id} className="mb-1 block text-xs font-medium text-muted">
        {label}
      </label>
      {children}
    </div>
  );
}

export function OrgForm({ org, initial, settingsRaw, labels }: { org: string; initial: State; settingsRaw: Record<string, unknown>; labels: Record<string, string> }) {
  const { run, error, busy } = useApi();
  const [s, setS] = useState(initial);
  const [saved, setSaved] = useState(false);
  const set = <K extends keyof State>(k: K, v: State[K]) => setS({ ...s, [k]: v });
  return (
    <form
      className="space-y-4"
      onSubmit={async (e) => {
        e.preventDefault();
        setSaved(false);
        const ok = await run("PATCH", `/api/v1/orgs/${org}`, {
          name: s.name,
          currency: s.currency,
          locale: s.locale,
          timezone: s.timezone,
          ...(s.vat ? { vat_rate: s.vat } : {}),
          settings: {
            ...settingsRaw,
            k8s_allocation_method: s.k8s,
            k8s_idle_mode: s.idle,
            prefer_invoice: s.preferInvoice,
            llm_provider: s.llm,
            allow_external_llm: s.external,
            report_recipients: s.recipients.split(",").map((r) => r.trim()).filter(Boolean),
            sso_idp_alias: s.sso,
            sso_enforced: s.ssoEnforced,
            rightsizing_percentile: s.percentile,
            rightsizing_window_days: s.window,
          },
        });
        if (ok) setSaved(true);
      }}
    >
      <Card>
        <CardHeader title={labels.org} />
        <CardBody className="grid grid-cols-1 gap-4 md:grid-cols-3">
          <Row id="o-name" label={labels.name}>
            <Input id="o-name" value={s.name} onChange={(e) => set("name", e.target.value)} required />
          </Row>
          <Row id="o-cur" label={labels.currency}>
            <Select id="o-cur" value={s.currency} onChange={(e) => set("currency", e.target.value)} className="w-full">
              {["EUR", "USD", "GBP", "CHF"].map((c) => (
                <option key={c}>{c}</option>
              ))}
            </Select>
          </Row>
          <Row id="o-loc" label={labels.language}>
            <Select id="o-loc" value={s.locale} onChange={(e) => set("locale", e.target.value)} className="w-full">
              <option value="fr">Français</option>
              <option value="en">English</option>
            </Select>
          </Row>
          <Row id="o-tz" label={labels.timezone}>
            <Input id="o-tz" value={s.timezone} onChange={(e) => set("timezone", e.target.value)} />
          </Row>
          <Row id="o-vat" label={labels.vat}>
            <Input id="o-vat" value={s.vat} placeholder="0.20" inputMode="decimal" onChange={(e) => set("vat", e.target.value)} />
          </Row>
          <Row id="o-k8s" label={labels.k8s}>
            <Select id="o-k8s" value={s.k8s} onChange={(e) => set("k8s", e.target.value)} className="w-full">
              <option value="max">max(requests, usage)</option>
              <option value="requests">requests</option>
              <option value="usage">usage</option>
            </Select>
          </Row>
          <Row id="o-idle" label={labels.idle}>
            <Select id="o-idle" value={s.idle} onChange={(e) => set("idle", e.target.value)} className="w-full">
              <option value="keep">Ligne idle par node</option>
              <option value="distribute">Réparti sur les pods</option>
            </Select>
          </Row>
          <Row id="o-pct" label="Percentile de rightsizing">
            <Input id="o-pct" type="number" min={50} max={100} value={s.percentile} onChange={(e) => set("percentile", Number(e.target.value))} />
          </Row>
          <Row id="o-win" label="Fenêtre de rightsizing (jours)">
            <Input id="o-win" type="number" min={3} max={90} value={s.window} onChange={(e) => set("window", Number(e.target.value))} />
          </Row>
          <label className="flex items-center gap-2 text-sm md:col-span-3">
            <input type="checkbox" checked={s.preferInvoice} onChange={(e) => set("preferInvoice", e.target.checked)} />
            {labels.preferInvoice}
          </label>
        </CardBody>
      </Card>
      <Card>
        <CardHeader title={labels.ai} description="Données hébergées dans l'UE. Aucun appel LLM hors UE sans autorisation explicite." />
        <CardBody className="grid grid-cols-1 gap-4 md:grid-cols-3">
          <Row id="o-llm" label={labels.llm}>
            <Select id="o-llm" value={s.llm} onChange={(e) => set("llm", e.target.value)} className="w-full">
              <option value="mistral">Mistral (UE)</option>
              <option value="local">Modèle local (API compatible OpenAI)</option>
              <option value="anthropic">Anthropic Claude</option>
              <option value="none">Désactivé</option>
            </Select>
          </Row>
          <label className="flex items-center gap-2 self-end pb-2 text-sm md:col-span-2">
            <input type="checkbox" checked={s.external} onChange={(e) => set("external", e.target.checked)} />
            {labels.external}
          </label>
          <Row id="o-rec" label={labels.recipients}>
            <Input id="o-rec" value={s.recipients} placeholder="dirigeant@exemple.fr, daf@exemple.fr" onChange={(e) => set("recipients", e.target.value)} />
          </Row>
        </CardBody>
      </Card>
      <Card>
        <CardHeader title={labels.security} />
        <CardBody className="grid grid-cols-1 gap-4 md:grid-cols-3">
          <Row id="o-sso" label={labels.sso}>
            <Input id="o-sso" value={s.sso} placeholder="acme-entra-id" onChange={(e) => set("sso", e.target.value)} />
          </Row>
          <label className="flex items-center gap-2 self-end pb-2 text-sm">
            <input type="checkbox" checked={s.ssoEnforced} onChange={(e) => set("ssoEnforced", e.target.checked)} />
            SSO obligatoire
          </label>
        </CardBody>
      </Card>
      <ErrorText error={error} />
      <div className="flex items-center gap-3">
        <Button type="submit" disabled={busy}>
          {labels.save}
        </Button>
        {saved ? (
          <span role="status" className="text-sm text-success">
            ✓
          </span>
        ) : null}
      </div>
    </form>
  );
}
