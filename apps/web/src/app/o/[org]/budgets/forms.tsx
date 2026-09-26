"use client";

import { useState } from "react";

import { ErrorText, useApi } from "@/components/actions";
import { Button, Input, Select } from "@/components/ui/primitives";

type Opt = { id: string; label: string };

export function BudgetForm({
  org,
  currency,
  nodes,
  channels,
  labels,
}: {
  org: string;
  currency: string;
  nodes: Opt[];
  channels: Opt[];
  labels: { title: string; name: string; amount: string; period: string; scope: string; whole: string; create: string; periods: Record<string, string>; channels: string };
}) {
  const { run, error, busy } = useApi();
  const [name, setName] = useState("");
  const [amount, setAmount] = useState("");
  const [period, setPeriod] = useState("monthly");
  const [node, setNode] = useState("");
  const [channel, setChannel] = useState("");
  return (
    <form
      className="space-y-2"
      onSubmit={async (e) => {
        e.preventDefault();
        const ok = await run("POST", `/api/v1/orgs/${org}/budgets`, {
          name,
          amount,
          period,
          currency,
          ...(node ? { node_id: node } : {}),
          ...(channel ? { channel_ids: [channel] } : {}),
        });
        if (ok) {
          setName("");
          setAmount("");
        }
      }}
    >
      <p className="text-xs font-semibold uppercase text-muted">{labels.title}</p>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-5">
        <Input aria-label={labels.name} placeholder={labels.name} value={name} onChange={(e) => setName(e.target.value)} required />
        <Input aria-label={labels.amount} placeholder={`${labels.amount} (${currency})`} inputMode="decimal" pattern="[0-9]+([.,][0-9]{1,2})?" value={amount} onChange={(e) => setAmount(e.target.value.replace(",", "."))} required />
        <Select aria-label={labels.period} value={period} onChange={(e) => setPeriod(e.target.value)}>
          {Object.entries(labels.periods).map(([k, v]) => (
            <option key={k} value={k}>
              {v}
            </option>
          ))}
        </Select>
        <Select aria-label={labels.scope} value={node} onChange={(e) => setNode(e.target.value)}>
          <option value="">{labels.whole}</option>
          {nodes.map((n) => (
            <option key={n.id} value={n.id}>
              {n.label}
            </option>
          ))}
        </Select>
        <Select aria-label={labels.channels} value={channel} onChange={(e) => setChannel(e.target.value)}>
          <option value="">— {labels.channels} —</option>
          {channels.map((c) => (
            <option key={c.id} value={c.id}>
              {c.label}
            </option>
          ))}
        </Select>
      </div>
      <ErrorText error={error} />
      <Button type="submit" size="sm" disabled={busy}>
        {labels.create}
      </Button>
    </form>
  );
}

const KINDS: { kind: string; secret: string; setting?: string }[] = [
  { kind: "slack", secret: "webhook_url", setting: "channel" },
  { kind: "teams", secret: "webhook_url" },
  { kind: "mattermost", secret: "webhook_url", setting: "channel" },
  { kind: "webhook", secret: "webhook_url" },
  { kind: "pagerduty", secret: "routing_key" },
  { kind: "email", secret: "", setting: "recipients" },
];

export function ChannelForm({ org, labels }: { org: string; labels: { name: string; create: string } }) {
  const { run, error, busy } = useApi();
  const [kind, setKind] = useState("slack");
  const [name, setName] = useState("");
  const [secret, setSecret] = useState("");
  const [setting, setSetting] = useState("");
  const spec = KINDS.find((k) => k.kind === kind)!;
  return (
    <form
      className="space-y-2"
      onSubmit={async (e) => {
        e.preventDefault();
        const ok = await run("POST", `/api/v1/orgs/${org}/channels`, {
          kind,
          name,
          settings: spec.setting && setting ? { [spec.setting]: setting } : {},
          secrets: spec.secret && secret ? { [spec.secret]: secret } : {},
        });
        if (ok) {
          setName("");
          setSecret("");
          setSetting("");
        }
      }}
    >
      <div className="grid grid-cols-1 gap-2 md:grid-cols-4">
        <Select aria-label="Type" value={kind} onChange={(e) => setKind(e.target.value)}>
          {KINDS.map((k) => (
            <option key={k.kind}>{k.kind}</option>
          ))}
        </Select>
        <Input aria-label={labels.name} placeholder={labels.name} value={name} onChange={(e) => setName(e.target.value)} required />
        {spec.secret ? <Input aria-label={spec.secret} placeholder={spec.secret} type="password" value={secret} onChange={(e) => setSecret(e.target.value)} required autoComplete="off" /> : null}
        {spec.setting ? <Input aria-label={spec.setting} placeholder={spec.setting} value={setting} onChange={(e) => setSetting(e.target.value)} required={kind === "email"} /> : null}
      </div>
      <ErrorText error={error} />
      <Button type="submit" size="sm" variant="secondary" disabled={busy}>
        {labels.create}
      </Button>
    </form>
  );
}
