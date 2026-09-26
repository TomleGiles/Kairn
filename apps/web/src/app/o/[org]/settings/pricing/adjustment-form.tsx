"use client";

import { useState } from "react";

import { ErrorText, useApi } from "@/components/actions";
import { Button, Input, Select } from "@/components/ui/primitives";

export function AdjustmentForm({ org }: { org: string }) {
  const { run, error, busy } = useApi();
  const [kind, setKind] = useState("discount");
  const [name, setName] = useState("");
  const [provider, setProvider] = useState("");
  const [sku, setSku] = useState("");
  const [value, setValue] = useState("");
  const [units, setUnits] = useState("");
  const [from, setFrom] = useState(new Date().toISOString().slice(0, 10));
  return (
    <form
      className="space-y-2"
      onSubmit={async (e) => {
        e.preventDefault();
        const body: Record<string, unknown> = { kind, name, provider, sku_pattern: sku, valid_from: `${from}T00:00:00Z` };
        if (kind === "discount") body.percent = value;
        else body.amount = value;
        if (kind === "commitment") body.covered_units = units;
        if (await run("POST", `/api/v1/orgs/${org}/pricing/adjustments`, body)) {
          setName("");
          setValue("");
        }
      }}
    >
      <p className="text-xs font-semibold uppercase text-muted">Nouvel ajustement</p>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-7">
        <Select aria-label="Type" value={kind} onChange={(e) => setKind(e.target.value)}>
          <option value="discount">Remise (%)</option>
          <option value="commitment">Engagement</option>
          <option value="credit">Crédit</option>
        </Select>
        <Input aria-label="Nom" placeholder="Nom" value={name} onChange={(e) => setName(e.target.value)} required />
        <Input aria-label="Fournisseur" placeholder="fournisseur (vide = tous)" value={provider} onChange={(e) => setProvider(e.target.value)} />
        <Input aria-label="SKU" placeholder="compute.flavor.*" value={sku} onChange={(e) => setSku(e.target.value)} required={kind === "commitment"} />
        <Input aria-label="Valeur" placeholder={kind === "discount" ? "%" : "montant"} inputMode="decimal" value={value} onChange={(e) => setValue(e.target.value.replace(",", "."))} required />
        {kind === "commitment" ? <Input aria-label="Unités couvertes" placeholder="unités couvertes" value={units} onChange={(e) => setUnits(e.target.value)} required /> : <span />}
        <Input aria-label="Valide depuis" type="date" value={from} onChange={(e) => setFrom(e.target.value)} required />
      </div>
      <ErrorText error={error} />
      <Button type="submit" size="sm" disabled={busy}>
        Ajouter
      </Button>
    </form>
  );
}
