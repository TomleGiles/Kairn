"use client";

import { useState } from "react";

import { ErrorText, useApi } from "@/components/actions";
import { Button, Input, Select } from "@/components/ui/primitives";

export function InviteForm({ org, roles, labels }: { org: string; roles: Record<string, string>; labels: { email: string; invite: string; role: string } }) {
  const { run, error, busy } = useApi();
  const [email, setEmail] = useState("");
  const [role, setRole] = useState("viewer");
  return (
    <form
      className="flex flex-wrap items-end gap-2"
      onSubmit={async (e) => {
        e.preventDefault();
        if (await run("POST", `/api/v1/orgs/${org}/members`, { email, role })) setEmail("");
      }}
    >
      <div className="w-72">
        <label htmlFor="inv-email" className="mb-1 block text-xs font-medium text-muted">
          {labels.email}
        </label>
        <Input id="inv-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
      </div>
      <Select aria-label={labels.role} value={role} onChange={(e) => setRole(e.target.value)}>
        {Object.entries(roles).map(([k, v]) => (
          <option key={k} value={k}>
            {v}
          </option>
        ))}
      </Select>
      <Button type="submit" disabled={busy}>
        {labels.invite}
      </Button>
      <ErrorText error={error} />
    </form>
  );
}

export function RoleSelect({ org, userId, role, roles }: { org: string; userId: string; role: string; roles: Record<string, string> }) {
  const { run, error, busy } = useApi();
  return (
    <span className="inline-flex flex-col">
      <Select aria-label="Rôle" value={role} disabled={busy} onChange={(e) => run("PUT", `/api/v1/orgs/${org}/members/${userId}`, { role: e.target.value })}>
        {Object.entries(roles).map(([k, v]) => (
          <option key={k} value={k}>
            {v}
          </option>
        ))}
      </Select>
      <ErrorText error={error} />
    </span>
  );
}
