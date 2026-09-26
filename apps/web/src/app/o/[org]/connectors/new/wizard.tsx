"use client";

import { CheckCircle2, ShieldCheck } from "lucide-react";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";

import { callApi, ErrorText } from "@/components/actions";
import { Badge, Button, Card, CardBody, CardHeader, Input } from "@/components/ui/primitives";

type Field = { name: string; label: string; secret: boolean; required: boolean; help: string; def: string };
type TypeInfo = {
  type: string;
  name: string;
  category: string;
  provider: string;
  resources: string[];
  fields: Field[];
  permissions: { scope: string; description: string; optional: boolean }[];
  interval: number;
  docs: string;
};

export function ConnectorWizard({
  org,
  types,
  labels,
}: {
  org: string;
  types: TypeInfo[];
  labels: {
    choose: string;
    configure: string;
    permissions: string;
    resources: string;
    validate: string;
    create: string;
    name: string;
    backfill: string;
    back: string;
    guide: string;
    categories: Record<string, string>;
  };
}) {
  const router = useRouter();
  const [sel, setSel] = useState<TypeInfo | null>(null);
  const [name, setName] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});
  const [backfill, setBackfill] = useState(30);
  const [error, setError] = useState<string | null>(null);
  const [check, setCheck] = useState<{ ok: boolean; error?: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const groups = useMemo(() => {
    const g = new Map<string, TypeInfo[]>();
    for (const ty of types) g.set(ty.category, [...(g.get(ty.category) ?? []), ty]);
    return Array.from(g.entries());
  }, [types]);
  const body = () => {
    const settings: Record<string, string> = {};
    const secrets: Record<string, string> = {};
    for (const f of sel?.fields ?? []) {
      const v = values[f.name] ?? f.def;
      if (!v) continue;
      (f.secret ? secrets : settings)[f.name] = v;
    }
    return { type: sel?.type, name: name || sel?.name, settings, secrets, backfill_days: backfill };
  };

  if (!sel) {
    return (
      <div className="space-y-6">
        <p className="text-sm font-medium">{labels.choose}</p>
        {groups.map(([cat, list]) => (
          <section key={cat} aria-labelledby={`cat-${cat}`}>
            <h2 id={`cat-${cat}`} className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">
              {labels.categories[cat] ?? cat}
            </h2>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
              {list.map((ty) => (
                <button
                  key={ty.type}
                  onClick={() => {
                    setSel(ty);
                    setValues(Object.fromEntries(ty.fields.map((f) => [f.name, f.def])));
                  }}
                  className="rounded-xl border border-border bg-surface p-4 text-left hover:border-brand focus-visible:border-brand"
                >
                  <p className="font-medium">{ty.name}</p>
                  <p className="mt-1 text-xs text-muted">{ty.resources.slice(0, 5).join(", ")}</p>
                </button>
              ))}
            </div>
          </section>
        ))}
      </div>
    );
  }
  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
      <Card className="xl:col-span-2">
        <CardHeader title={`${labels.configure} — ${sel.name}`} action={<Button variant="ghost" size="sm" onClick={() => { setSel(null); setCheck(null); }}>← {labels.back}</Button>} />
        <CardBody>
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              setError(null);
              const res = await callApi<{ id: string }>("POST", `/api/v1/orgs/${org}/connectors`, body());
              setBusy(false);
              if (res.error) setError(res.error);
              else router.push(`/o/${org}/connectors/${res.data?.id}`);
            }}
          >
            <div>
              <label htmlFor="c-name" className="mb-1 block text-xs font-medium text-muted">
                {labels.name}
              </label>
              <Input id="c-name" value={name} placeholder={sel.name} onChange={(e) => setName(e.target.value)} />
            </div>
            {sel.fields.map((f) => (
              <div key={f.name}>
                <label htmlFor={`f-${f.name}`} className="mb-1 block text-xs font-medium text-muted">
                  {f.label}
                  {f.required ? " *" : ""}
                </label>
                <Input
                  id={`f-${f.name}`}
                  type={f.secret ? "password" : "text"}
                  autoComplete="off"
                  required={f.required}
                  value={values[f.name] ?? ""}
                  onChange={(e) => setValues({ ...values, [f.name]: e.target.value })}
                />
                {f.help ? <p className="mt-1 text-xs text-subtle">{f.help}</p> : null}
              </div>
            ))}
            <div className="w-40">
              <label htmlFor="c-backfill" className="mb-1 block text-xs font-medium text-muted">
                {labels.backfill}
              </label>
              <Input id="c-backfill" type="number" min={0} max={396} value={backfill} onChange={(e) => setBackfill(Number(e.target.value))} />
            </div>
            <ErrorText error={error} />
            {check ? (
              <p role="status" className={`flex items-center gap-2 text-sm ${check.ok ? "text-success" : "text-danger"}`}>
                {check.ok ? <CheckCircle2 className="size-4" /> : null}
                {check.ok ? "Accès vérifié" : check.error}
              </p>
            ) : null}
            <div className="flex gap-2">
              <Button
                type="button"
                variant="secondary"
                disabled={busy}
                onClick={async () => {
                  setBusy(true);
                  const res = await callApi<{ ok: boolean; error?: string }>("POST", `/api/v1/orgs/${org}/connectors/validate`, body());
                  setBusy(false);
                  setCheck(res.data ?? { ok: false, error: res.error });
                }}
              >
                {labels.validate}
              </Button>
              <Button type="submit" disabled={busy}>
                {labels.create}
              </Button>
            </div>
          </form>
        </CardBody>
      </Card>
      <Card>
        <CardHeader title={labels.permissions} />
        <CardBody className="space-y-3">
          <ul className="space-y-2 text-sm">
            {sel.permissions.map((p) => (
              <li key={p.scope} className="flex gap-2">
                <ShieldCheck className="mt-0.5 size-4 shrink-0 text-brand" aria-hidden />
                <span>
                  <code className="text-xs">{p.scope}</code>
                  {p.optional ? <Badge className="ml-1">optionnel</Badge> : null}
                  <br />
                  <span className="text-xs text-muted">{p.description}</span>
                </span>
              </li>
            ))}
          </ul>
          <div>
            <p className="text-xs font-medium uppercase text-muted">{labels.resources}</p>
            <div className="mt-1 flex flex-wrap gap-1">
              {sel.resources.map((r) => (
                <Badge key={r}>{r}</Badge>
              ))}
            </div>
          </div>
          {sel.docs ? (
            <a href={sel.docs} target="_blank" rel="noopener" className="inline-block text-sm text-brand hover:underline">
              {labels.guide}
            </a>
          ) : null}
        </CardBody>
      </Card>
    </div>
  );
}
