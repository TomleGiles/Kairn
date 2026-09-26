"use client";

import { useState } from "react";

import { ErrorText, useApi } from "@/components/actions";
import { Button, Input } from "@/components/ui/primitives";

export function ClientForm({ org }: { org: string }) {
  const { run, error, busy } = useApi();
  const [name, setName] = useState("");
  return (
    <form
      className="flex flex-wrap items-end gap-2"
      onSubmit={async (e) => {
        e.preventDefault();
        if (await run("POST", `/api/v1/orgs/${org}/clients`, { name })) setName("");
      }}
    >
      <div className="w-72">
        <label htmlFor="cl-name" className="mb-1 block text-xs font-medium text-muted">
          Nouveau client
        </label>
        <Input id="cl-name" value={name} onChange={(e) => setName(e.target.value)} required minLength={2} />
      </div>
      <Button type="submit" disabled={busy}>
        Créer
      </Button>
      <ErrorText error={error} />
    </form>
  );
}
