"use client";

import { useState } from "react";

import { ErrorText, useApi } from "@/components/actions";
import { Badge, Button, Input, Select, Table, Td, Th } from "@/components/ui/primitives";
import { formatMoney, type Locale } from "@/lib/format";

type Result = {
  currency: string;
  current_monthly: string;
  projected_monthly: string;
  delta_monthly: string;
  delta_percent: string;
  lines: { resource_id?: string; name: string; from: string; to: string; current_monthly: string; projected_monthly: string; delta_monthly: string }[];
  unmapped: string[];
  assumptions: string[];
};

const PROVIDERS = ["openstack", "scaleway", "outscale"];

export function WhatIf({
  org,
  vms,
  locale,
  labels,
}: {
  org: string;
  vms: { id: string; name: string; provider: string; flavor: string }[];
  locale: Locale;
  labels: Record<"addNodes" | "changeFlavor" | "migrate" | "simulate" | "current" | "projected" | "delta" | "assumptions" | "unmapped" | "count" | "flavor" | "target" | "provider", string>;
}) {
  const { run, error, busy } = useApi();
  const [kind, setKind] = useState<"add_nodes" | "change_flavor" | "migrate">("add_nodes");
  const [provider, setProvider] = useState("openstack");
  const [flavor, setFlavor] = useState("b2-15");
  const [count, setCount] = useState(2);
  const [vm, setVm] = useState(vms[0]?.id ?? "");
  const [target, setTarget] = useState("scaleway");
  const [res, setRes] = useState<Result | null>(null);
  const scenario = () => {
    switch (kind) {
      case "add_nodes":
        return { kind, provider, flavor, count };
      case "change_flavor":
        return { kind, flavor, resource_ids: [vm] };
      default:
        return { kind, provider, target_provider: target };
    }
  };
  const m = (v?: string) => formatMoney(v, res?.currency ?? "EUR", locale);
  return (
    <div className="space-y-4">
      <form
        className="flex flex-wrap items-end gap-2"
        onSubmit={async (e) => {
          e.preventDefault();
          const r = await run<Result>("POST", `/api/v1/orgs/${org}/whatif`, { scenarios: [scenario()] }, false);
          if (r) setRes(r);
        }}
      >
        <Select aria-label="Scénario" value={kind} onChange={(e) => setKind(e.target.value as typeof kind)}>
          <option value="add_nodes">{labels.addNodes}</option>
          <option value="change_flavor">{labels.changeFlavor}</option>
          <option value="migrate">{labels.migrate}</option>
        </Select>
        {kind === "add_nodes" ? (
          <>
            <Select aria-label={labels.provider} value={provider} onChange={(e) => setProvider(e.target.value)}>
              {PROVIDERS.map((p) => (
                <option key={p}>{p}</option>
              ))}
            </Select>
            <Input aria-label={labels.flavor} className="w-36" value={flavor} onChange={(e) => setFlavor(e.target.value)} />
            <Input aria-label={labels.count} className="w-20" type="number" min={1} value={count} onChange={(e) => setCount(Number(e.target.value))} />
          </>
        ) : null}
        {kind === "change_flavor" ? (
          <>
            <Select aria-label="VM" value={vm} onChange={(e) => setVm(e.target.value)} className="max-w-64">
              {vms.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.name} ({v.flavor})
                </option>
              ))}
            </Select>
            <Input aria-label={labels.flavor} className="w-36" value={flavor} onChange={(e) => setFlavor(e.target.value)} />
          </>
        ) : null}
        {kind === "migrate" ? (
          <>
            <Select aria-label={labels.provider} value={provider} onChange={(e) => setProvider(e.target.value)}>
              {PROVIDERS.map((p) => (
                <option key={p}>{p}</option>
              ))}
            </Select>
            <span className="pb-2 text-sm text-muted">→</span>
            <Select aria-label={labels.target} value={target} onChange={(e) => setTarget(e.target.value)}>
              {PROVIDERS.filter((p) => p !== provider).map((p) => (
                <option key={p}>{p}</option>
              ))}
            </Select>
          </>
        ) : null}
        <Button type="submit" disabled={busy}>
          {labels.simulate}
        </Button>
      </form>
      <ErrorText error={error} />
      {res ? (
        <div className="space-y-3" aria-live="polite">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div className="rounded-lg border border-border p-4">
              <p className="text-xs uppercase text-muted">{labels.current}</p>
              <p className="text-xl font-semibold tabular">{m(res.current_monthly)}</p>
            </div>
            <div className="rounded-lg border border-border p-4">
              <p className="text-xs uppercase text-muted">{labels.projected}</p>
              <p className="text-xl font-semibold tabular">{m(res.projected_monthly)}</p>
            </div>
            <div className="rounded-lg border border-border p-4">
              <p className="text-xs uppercase text-muted">{labels.delta}</p>
              <p className={`text-xl font-semibold tabular ${res.delta_monthly.startsWith("-") ? "text-success" : "text-danger"}`}>
                {m(res.delta_monthly)} <span className="text-sm">({res.delta_percent} %)</span>
              </p>
            </div>
          </div>
          <Table>
            <thead>
              <tr>
                <Th />
                <Th>De</Th>
                <Th>Vers</Th>
                <Th align="right">{labels.current}</Th>
                <Th align="right">{labels.projected}</Th>
                <Th align="right">{labels.delta}</Th>
              </tr>
            </thead>
            <tbody>
              {res.lines.map((l, i) => (
                <tr key={i}>
                  <Td>{l.name}</Td>
                  <Td className="text-muted">{l.from}</Td>
                  <Td>{l.to}</Td>
                  <Td align="right">{m(l.current_monthly)}</Td>
                  <Td align="right">{m(l.projected_monthly)}</Td>
                  <Td align="right">{m(l.delta_monthly)}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
          {res.unmapped.length ? (
            <p className="text-sm text-muted">
              {labels.unmapped} : {res.unmapped.join(", ")}
            </p>
          ) : null}
          <div className="flex flex-wrap gap-1">
            <span className="text-xs text-muted">{labels.assumptions} :</span>
            {res.assumptions.map((a) => (
              <Badge key={a}>{a}</Badge>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}
