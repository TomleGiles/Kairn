"use client";

import { useState } from "react";

import { ErrorText, useApi } from "@/components/actions";
import { Button, Input } from "@/components/ui/primitives";

const NEXT: Record<string, string[]> = {
  open: ["accepted", "postponed", "dismissed"],
  accepted: ["applied", "dismissed", "open"],
  postponed: ["open", "accepted", "dismissed"],
  dismissed: ["open"],
  applied: [],
};

export function RecoActions({
  org,
  id,
  status,
  labels,
}: {
  org: string;
  id: string;
  status: string;
  labels: Record<"accept" | "postpone" | "dismiss" | "applied" | "reopen" | "reason" | "until" | "cancel", string>;
}) {
  const { run, error, busy } = useApi();
  const [pending, setPending] = useState<string | null>(null);
  const [reason, setReason] = useState("");
  const [until, setUntil] = useState("");
  const label: Record<string, string> = { accepted: labels.accept, postponed: labels.postpone, dismissed: labels.dismiss, applied: labels.applied, open: labels.reopen };
  const send = (to: string) =>
    run("POST", `/api/v1/orgs/${org}/recommendations/${id}/status`, {
      status: to,
      ...(reason ? { reason } : {}),
      ...(to === "postponed" && until ? { postponed_until: new Date(until).toISOString() } : {}),
    });
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2">
        {(NEXT[status] ?? []).map((to) => (
          <Button
            key={to}
            variant={to === "accepted" || to === "applied" ? "primary" : "secondary"}
            size="sm"
            disabled={busy}
            onClick={() => {
              if (to === "postponed" || to === "dismissed") setPending(to);
              else void send(to);
            }}
          >
            {label[to]}
          </Button>
        ))}
      </div>
      {pending ? (
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={async (e) => {
            e.preventDefault();
            const ok = await send(pending);
            if (ok) setPending(null);
          }}
        >
          <div className="min-w-64 flex-1">
            <label htmlFor="reason" className="mb-1 block text-xs text-muted">
              {labels.reason}
            </label>
            <Input id="reason" value={reason} onChange={(e) => setReason(e.target.value)} required />
          </div>
          {pending === "postponed" ? (
            <div>
              <label htmlFor="until" className="mb-1 block text-xs text-muted">
                {labels.until}
              </label>
              <Input id="until" type="date" value={until} onChange={(e) => setUntil(e.target.value)} required />
            </div>
          ) : null}
          <Button type="submit" size="sm" disabled={busy}>
            {label[pending]}
          </Button>
          <Button type="button" variant="ghost" size="sm" onClick={() => setPending(null)}>
            {labels.cancel}
          </Button>
        </form>
      ) : null}
      <ErrorText error={error} />
    </div>
  );
}
