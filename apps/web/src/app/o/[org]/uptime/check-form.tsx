"use client";

import { useState } from "react";

import { ErrorText, useApi } from "@/components/actions";
import { Button, Input, Select } from "@/components/ui/primitives";

const REGIONS = ["eu-west-gra", "eu-west-par", "eu-central-waw", "eu-west-rbx"];

export function CheckForm({ org, labels }: { org: string; labels: { title: string; name: string; target: string; create: string } }) {
  const { run, error, busy } = useApi();
  const [name, setName] = useState("");
  const [kind, setKind] = useState("http");
  const [target, setTarget] = useState("https://");
  const [regions, setRegions] = useState<string[]>(REGIONS.slice(0, 2));
  return (
    <form
      className="space-y-2"
      onSubmit={async (e) => {
        e.preventDefault();
        const ok = await run("POST", `/api/v1/orgs/${org}/uptime/checks`, { name, kind, target, regions });
        if (ok) {
          setName("");
          setTarget("https://");
        }
      }}
    >
      <p className="text-xs font-semibold uppercase text-muted">{labels.title}</p>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-4">
        <Input aria-label={labels.name} placeholder={labels.name} value={name} onChange={(e) => setName(e.target.value)} required />
        <Select aria-label="Type" value={kind} onChange={(e) => setKind(e.target.value)}>
          <option value="http">HTTP(S)</option>
          <option value="tcp">TCP</option>
          <option value="icmp">ICMP</option>
        </Select>
        <Input aria-label={labels.target} value={target} onChange={(e) => setTarget(e.target.value)} required className="md:col-span-2" />
      </div>
      <fieldset className="flex flex-wrap gap-3 text-sm">
        <legend className="sr-only">Régions</legend>
        {REGIONS.map((r) => (
          <label key={r} className="flex items-center gap-1.5">
            <input type="checkbox" checked={regions.includes(r)} onChange={(e) => setRegions(e.target.checked ? [...regions, r] : regions.filter((x) => x !== r))} />
            {r}
          </label>
        ))}
      </fieldset>
      <ErrorText error={error} />
      <Button type="submit" size="sm" disabled={busy || regions.length === 0}>
        {labels.create}
      </Button>
    </form>
  );
}
