"use client";

import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";

import { ErrorText, useApi } from "@/components/actions";
import { Button, Input, Select } from "@/components/ui/primitives";

type Node = { id: string; label: string; kind: string };
const KINDS = ["organization", "business_unit", "team", "service", "environment"];
const OPS = ["eq", "neq", "in", "regex", "prefix", "exists"];

export function NodeForm({ org, nodes, labels }: { org: string; nodes: Node[]; labels: { title: string; name: string; create: string; parent: string } }) {
  const { run, error, busy } = useApi();
  const [name, setName] = useState("");
  const [parent, setParent] = useState(nodes[0]?.id ?? "");
  const [kind, setKind] = useState("team");
  return (
    <form
      className="space-y-2"
      onSubmit={async (e) => {
        e.preventDefault();
        const ok = await run("POST", `/api/v1/orgs/${org}/allocation/nodes`, { name, kind, ...(parent ? { parent_id: parent } : {}) });
        if (ok) setName("");
      }}
    >
      <p className="text-xs font-semibold uppercase text-muted">{labels.title}</p>
      <Input aria-label={labels.name} placeholder={labels.name} value={name} onChange={(e) => setName(e.target.value)} required />
      <div className="flex gap-2">
        <Select aria-label={labels.parent} value={parent} onChange={(e) => setParent(e.target.value)} className="min-w-0 flex-1">
          <option value="">—</option>
          {nodes.map((n) => (
            <option key={n.id} value={n.id}>
              {n.label}
            </option>
          ))}
        </Select>
        <Select aria-label="Type" value={kind} onChange={(e) => setKind(e.target.value)}>
          {KINDS.map((k) => (
            <option key={k}>{k}</option>
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

type Cond = { field: string; op: string; value: string };

export function RuleForm({
  org,
  nodes,
  labels,
}: {
  org: string;
  nodes: Node[];
  labels: { title: string; name: string; priority: string; node: string; preview: string; matched: string; create: string };
}) {
  const { run, error, busy } = useApi();
  const [name, setName] = useState("");
  const [priority, setPriority] = useState(100);
  const [node, setNode] = useState(nodes[0]?.id ?? "");
  const [conds, setConds] = useState<Cond[]>([{ field: "label.team", op: "eq", value: "" }]);
  const [preview, setPreview] = useState<{ matched: number; sample: { id: string; name: string }[] } | null>(null);
  const payload = () =>
    conds.map((c) => (c.op === "in" ? { field: c.field, op: c.op, values: c.value.split(",").map((v) => v.trim()).filter(Boolean) } : { field: c.field, op: c.op, value: c.value }));
  return (
    <form
      className="space-y-3"
      onSubmit={async (e) => {
        e.preventDefault();
        const ok = await run("POST", `/api/v1/orgs/${org}/allocation/rules`, { name, priority, node_id: node, conditions: payload() });
        if (ok) {
          setName("");
          setPreview(null);
        }
      }}
    >
      <p className="text-xs font-semibold uppercase text-muted">{labels.title}</p>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-4">
        <Input aria-label={labels.name} placeholder={labels.name} value={name} onChange={(e) => setName(e.target.value)} required className="md:col-span-2" />
        <Input aria-label={labels.priority} type="number" min={0} value={priority} onChange={(e) => setPriority(Number(e.target.value))} />
        <Select aria-label={labels.node} value={node} onChange={(e) => setNode(e.target.value)}>
          {nodes.map((n) => (
            <option key={n.id} value={n.id}>
              {n.label}
            </option>
          ))}
        </Select>
      </div>
      {conds.map((c, i) => (
        <div key={i} className="flex gap-2">
          <Input aria-label="Champ" value={c.field} onChange={(e) => setConds(conds.map((x, j) => (j === i ? { ...x, field: e.target.value } : x)))} placeholder="label.team, attr.k8s.namespace, provider…" />
          <Select aria-label="Opérateur" value={c.op} onChange={(e) => setConds(conds.map((x, j) => (j === i ? { ...x, op: e.target.value } : x)))}>
            {OPS.map((o) => (
              <option key={o}>{o}</option>
            ))}
          </Select>
          <Input aria-label="Valeur" value={c.value} onChange={(e) => setConds(conds.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))} placeholder="valeur (in : a, b, c)" />
          <Button type="button" variant="ghost" size="icon" aria-label="Retirer la condition" onClick={() => setConds(conds.filter((_, j) => j !== i))} disabled={conds.length === 1}>
            <Trash2 />
          </Button>
        </div>
      ))}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" variant="secondary" size="sm" onClick={() => setConds([...conds, { field: "", op: "eq", value: "" }])}>
          <Plus /> Condition
        </Button>
        <Button
          type="button"
          variant="secondary"
          size="sm"
          disabled={busy}
          onClick={async () => {
            const res = await run<{ matched: number; sample: { id: string; name: string }[] }>("POST", `/api/v1/orgs/${org}/allocation/rules/preview`, { conditions: payload() }, false);
            if (res) setPreview(res);
          }}
        >
          {labels.preview}
        </Button>
        <Button type="submit" size="sm" disabled={busy || !node}>
          {labels.create}
        </Button>
        {preview ? (
          <span className="text-sm text-muted" aria-live="polite">
            {preview.matched} {labels.matched}
            {preview.sample.length ? ` : ${preview.sample.slice(0, 5).map((s) => s.name).join(", ")}…` : ""}
          </span>
        ) : null}
      </div>
      <ErrorText error={error} />
    </form>
  );
}
