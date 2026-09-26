"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import { callApi, ErrorText } from "@/components/actions";
import { Button, Card, CardBody, Input, Select } from "@/components/ui/primitives";

export function OnboardingForm({ labels }: { labels: { orgName: string; create: string } }) {
  const router = useRouter();
  const [name, setName] = useState("");
  const [currency, setCurrency] = useState("EUR");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  return (
    <Card>
      <CardBody>
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            const res = await callApi<{ id: string }>("POST", "/api/v1/orgs", { name, currency });
            setBusy(false);
            if (res.error) return setError(res.error);
            router.push(`/o/${res.data?.id}/connectors/new`);
          }}
        >
          <div>
            <label htmlFor="ob-name" className="mb-1 block text-xs font-medium text-muted">
              {labels.orgName}
            </label>
            <Input id="ob-name" value={name} onChange={(e) => setName(e.target.value)} required minLength={2} />
          </div>
          <Select aria-label="Devise" value={currency} onChange={(e) => setCurrency(e.target.value)}>
            {["EUR", "USD", "GBP", "CHF"].map((c) => (
              <option key={c}>{c}</option>
            ))}
          </Select>
          <ErrorText error={error} />
          <Button type="submit" disabled={busy} className="w-full">
            {labels.create}
          </Button>
        </form>
      </CardBody>
    </Card>
  );
}
