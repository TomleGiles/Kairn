"use client";

import { useState } from "react";

import { CopyButton, ErrorText, useApi } from "@/components/actions";
import { Button, Code, Input, Select } from "@/components/ui/primitives";

const SCOPES = ["costs:read", "org:read", "connectors:read", "reports:read", "export", "recommendations:manage", "scim"];

export function TokenForm({ org, roles, labels }: { org: string; roles: Record<string, string>; labels: Record<"name" | "role" | "create" | "once" | "copy" | "copied", string> }) {
  const { run, error, busy } = useApi();
  const [name, setName] = useState("");
  const [role, setRole] = useState("viewer");
  const [scopes, setScopes] = useState<string[]>([]);
  const [days, setDays] = useState(90);
  const [created, setCreated] = useState<string | null>(null);
  return (
    <div className="space-y-3">
      <form
        className="flex flex-wrap items-end gap-2"
        onSubmit={async (e) => {
          e.preventDefault();
          const res = await run<{ token: string }>("POST", `/api/v1/orgs/${org}/tokens`, { name, role, scopes, expires_in_days: days });
          if (res?.token) {
            setCreated(res.token);
            setName("");
          }
        }}
      >
        <div className="w-56">
          <label htmlFor="tk-name" className="mb-1 block text-xs font-medium text-muted">
            {labels.name}
          </label>
          <Input id="tk-name" value={name} onChange={(e) => setName(e.target.value)} required />
        </div>
        <Select aria-label={labels.role} value={role} onChange={(e) => setRole(e.target.value)}>
          {Object.entries(roles)
            .filter(([k]) => k !== "owner")
            .map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
        </Select>
        <div className="w-24">
          <label htmlFor="tk-days" className="mb-1 block text-xs font-medium text-muted">
            Jours
          </label>
          <Input id="tk-days" type="number" min={0} max={730} value={days} onChange={(e) => setDays(Number(e.target.value))} />
        </div>
        <Button type="submit" disabled={busy}>
          {labels.create}
        </Button>
      </form>
      <fieldset className="flex flex-wrap gap-3 text-xs">
        <legend className="mb-1 text-xs font-medium text-muted">Scopes (vide = toutes les permissions du rôle)</legend>
        {SCOPES.map((s) => (
          <label key={s} className="flex items-center gap-1">
            <input type="checkbox" checked={scopes.includes(s)} onChange={(e) => setScopes(e.target.checked ? [...scopes, s] : scopes.filter((x) => x !== s))} />
            <code>{s}</code>
          </label>
        ))}
      </fieldset>
      <ErrorText error={error} />
      {created ? (
        <div role="status" className="space-y-2 rounded-lg border border-warning bg-warning-soft p-3">
          <p className="text-sm font-medium text-warning">{labels.once}</p>
          <Code>{created}</Code>
          <CopyButton value={created} label={labels.copy} copied={labels.copied} />
        </div>
      ) : null}
    </div>
  );
}
