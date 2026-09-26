import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { UptimeBars } from "@/app/o/[org]/uptime/uptime-bars";
import { formatDate, formatPercent } from "@/lib/format";
import { getI18n } from "@/lib/i18n";

const API_URL = process.env.KAIRN_API_URL ?? "http://localhost:8080";

type Status = {
  title: string;
  branding: Record<string, string>;
  overall: "operational" | "degraded" | "outage";
  components: { check_id: string; name: string; up?: boolean | null; uptime_30d: number; daily: { day: string; uptime: number }[] }[];
  incidents: { id: string; title: string; status: string; started_at: string; resolved_at?: string; updates: { at: string; status: string; message: string }[] }[];
  generated_at: string;
};

async function load(slug: string, token?: string): Promise<Status | null> {
  const q = token ? `?token=${encodeURIComponent(token)}` : "";
  const res = await fetch(`${API_URL}/api/v1/public/status/${encodeURIComponent(slug)}${q}`, { next: { revalidate: 30 } });
  if (!res.ok) return null;
  return (await res.json()) as Status;
}

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
  const { slug } = await params;
  const s = await load(slug);
  return { title: s?.title ?? "Statut", robots: { index: true, follow: false } };
}

export default async function PublicStatusPage({ params, searchParams }: { params: Promise<{ slug: string }>; searchParams: Promise<{ token?: string }> }) {
  const { slug } = await params;
  const { token } = await searchParams;
  const { locale, t } = await getI18n();
  const s = await load(slug, token);
  if (!s) notFound();
  const color = s.branding?.primary_color || "var(--brand)";
  const banner = {
    operational: { text: t.uptime.operational, cls: "bg-success-soft text-success" },
    degraded: { text: t.uptime.degraded, cls: "bg-warning-soft text-warning" },
    outage: { text: t.uptime.outage, cls: "bg-danger-soft text-danger" },
  }[s.overall];
  return (
    <main id="main" className="mx-auto max-w-3xl px-4 py-10">
      <header className="mb-8 flex items-center gap-3">
        {s.branding?.logo_url ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img src={s.branding.logo_url} alt="" className="h-8" />
        ) : (
          <span className="size-8 rounded-lg" style={{ background: color }} aria-hidden />
        )}
        <h1 className="text-xl font-semibold">{s.title}</h1>
      </header>
      <p role="status" className={`rounded-xl px-5 py-4 text-sm font-medium ${banner.cls}`}>
        {banner.text}
      </p>
      <section aria-labelledby="components" className="mt-8 rounded-xl border border-border bg-surface">
        <h2 id="components" className="sr-only">
          Services
        </h2>
        <ul className="divide-y divide-border">
          {s.components.map((c) => (
            <li key={c.check_id} className="px-5 py-4">
              <div className="mb-2 flex items-center justify-between text-sm">
                <span className="font-medium">{c.name}</span>
                <span className={c.up === false ? "text-danger" : "text-success"}>{c.up === false ? t.uptime.down : t.uptime.up}</span>
              </div>
              <UptimeBars days={c.daily} />
              <p className="mt-1 flex justify-between text-xs text-subtle">
                <span>{t.uptime.last90}</span>
                <span>{formatPercent(c.uptime_30d, locale, 3)} (30 j)</span>
              </p>
            </li>
          ))}
        </ul>
      </section>
      <section aria-labelledby="incidents" className="mt-8">
        <h2 id="incidents" className="mb-3 text-sm font-semibold">
          {t.uptime.incidents}
        </h2>
        {s.incidents.length === 0 ? <p className="text-sm text-muted">—</p> : null}
        <ul className="space-y-4">
          {s.incidents.map((i) => (
            <li key={i.id} className="rounded-xl border border-border bg-surface p-4">
              <p className="font-medium">{i.title}</p>
              <ol className="mt-2 space-y-1 text-sm">
                {i.updates.map((u, k) => (
                  <li key={k}>
                    <span className="text-xs text-subtle">{formatDate(u.at, locale, true)}</span> — <strong>{u.status}</strong> : {u.message}
                  </li>
                ))}
              </ol>
            </li>
          ))}
        </ul>
      </section>
      <footer className="mt-10 text-center text-xs text-subtle">
        {t.uptime.poweredBy} · {formatDate(s.generated_at, locale, true)}
      </footer>
    </main>
  );
}
