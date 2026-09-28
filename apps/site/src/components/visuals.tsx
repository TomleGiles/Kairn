import { Icon } from "./icons";
import { drilldown, invoiceLines, invoiceTotal, teamCosts } from "@/lib/content";

// Illustrations du site vitrine, en HTML/SVG (aucune image à charger).
// Toutes utilisent les données de démonstration de lib/content.ts.

function DemoNote({ className = "" }: { className?: string }) {
  return <p className={`mt-3 text-xs ${className}`}>Illustration — données de démonstration.</p>;
}

/** Facture brute face à la même facture expliquée par équipe. */
export function InvoiceCompare() {
  return (
    <div className="grid items-stretch gap-6 lg:grid-cols-[1fr_auto_1fr]">
      <figure className="reveal-left rounded-2xl border border-border bg-card p-6 shadow-sm" aria-labelledby="facture-brute">
        <figcaption id="facture-brute" className="flex items-center justify-between text-sm">
          <span className="font-semibold">Ce que dit votre facture</span>
          <span className="rounded-full bg-bg-2 px-2.5 py-0.5 text-xs text-muted">PDF fournisseur</span>
        </figcaption>
        <dl className="mt-5 space-y-3 font-mono text-[13px]">
          {invoiceLines.map((l) => (
            <div key={l.label} className="flex justify-between gap-4 border-b border-dashed border-border pb-3 text-muted">
              <dt>{l.label}</dt>
              <dd className="shrink-0 tabular-nums">{l.amount}</dd>
            </div>
          ))}
          <div className="flex justify-between gap-4 pt-1 font-semibold">
            <dt>Total HT</dt>
            <dd className="tabular-nums">{invoiceTotal}</dd>
          </div>
        </dl>
        <p className="mt-6 rounded-lg bg-bg-2 p-3 text-sm text-muted">
          Qui a consommé ces 31 480 € d&apos;instances ? Pourquoi +6 % ce mois-ci ? La facture ne le dit pas.
        </p>
      </figure>

      <div className="flex items-center justify-center" aria-hidden="true">
        <span className="reveal-scale inline-flex size-12 items-center justify-center rounded-full bg-brand text-brand-fg shadow-lg shadow-teal-500/30 lg:size-14">
          <Icon name="arrow" className="size-6 rotate-90 lg:rotate-0" />
        </span>
      </div>

      <figure className="reveal rounded-2xl border border-brand/40 bg-card p-6 shadow-xl shadow-teal-900/10 ring-1 ring-brand/20" aria-labelledby="facture-kairn">
        <figcaption id="facture-kairn" className="flex items-center justify-between text-sm">
          <span className="font-semibold">Ce que dit Kairn</span>
          <span className="rounded-full bg-brand-soft px-2.5 py-0.5 text-xs font-medium text-brand">96,4 % alloué</span>
        </figcaption>
        <div className="mt-5 flex h-3 overflow-hidden rounded-full" aria-hidden="true">
          {teamCosts.map((t) => (
            <span key={t.team} className={`fill-bar ${t.color}`} style={{ width: `${t.share}%` }} />
          ))}
        </div>
        <ul className="mt-5 space-y-2.5 text-sm">
          {teamCosts.map((t) => (
            <li key={t.team} className="flex items-center gap-3">
              <span className={`size-2.5 shrink-0 rounded-sm ${t.color}`} aria-hidden="true" />
              <span className="font-medium">{t.team}</span>
              <span className="truncate text-xs text-subtle">{t.note}</span>
              <span className="ml-auto shrink-0 font-semibold tabular-nums">{t.amount}</span>
            </li>
          ))}
        </ul>
        <p className="mt-5 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm">
          <strong>Équipe Data +28 %</strong> : 12 volumes créés par le déploiement <code className="font-mono text-xs">etl-v2.3</code>, dont 9 orphelins.
        </p>
      </figure>
    </div>
  );
}

/** Descente de la facture jusqu'au pod, puis à l'équipe qui paie. */
export function Drilldown() {
  return (
    <figure className="cascade relative mx-auto max-w-4xl" aria-label="Descente d'un coût, de la facture jusqu'au pod puis à l'équipe">
      {/* Circuit vertical qui se remplit au défilement. */}
      <div className="absolute top-6 bottom-6 left-[1.4rem] w-px bg-ink-border sm:left-1/2" aria-hidden="true">
        <div className="cascade-line absolute inset-0 bg-gradient-to-b from-teal-300 via-indigo-400 to-teal-300" />
      </div>
      <ol className="relative space-y-8 sm:space-y-4">
        {drilldown.map((d, i) => {
          const right = i % 2 === 1;
          return (
            <li key={d.level} className="relative grid items-center gap-4 pl-14 sm:grid-cols-2 sm:gap-16 sm:pl-0">
              <span
                className="absolute top-5 left-0 z-10 inline-flex size-11 items-center justify-center rounded-full border border-teal-300/40 bg-ink-2 font-mono text-sm font-semibold text-teal-200 shadow-lg shadow-teal-500/20 sm:top-1/2 sm:left-1/2 sm:-translate-x-1/2 sm:-translate-y-1/2"
                aria-hidden="true"
              >
                {i + 1}
              </span>
              <div className={`${right ? "sm:col-start-2" : "sm:text-right"} ${right ? "reveal" : "reveal-left"}`}>
                <p className="text-xs font-semibold tracking-widest text-teal-300 uppercase">{d.level}</p>
                <p className="mt-1 text-lg font-semibold text-ink-fg">{d.name}</p>
                <p className="mt-1 text-sm text-ink-muted">{d.detail}</p>
              </div>
              <div className={`${right ? "sm:col-start-1 sm:row-start-1" : ""} reveal-scale`}>
                <div className="rounded-2xl border border-ink-border bg-ink-2/80 p-4 backdrop-blur">
                  <div className="flex items-baseline justify-between gap-3">
                    <span className="text-2xl font-semibold tracking-tight text-ink-fg tabular-nums">{d.amount}</span>
                    <span className="rounded-full bg-white/5 px-2.5 py-0.5 font-mono text-[11px] text-ink-muted">{d.source}</span>
                  </div>
                  <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-white/5" aria-hidden="true">
                    <div
                      className="fill-bar h-full rounded-full bg-gradient-to-r from-teal-300 to-indigo-400"
                      style={{ width: `${Math.max(d.share, 2)}%` }}
                    />
                  </div>
                </div>
              </div>
            </li>
          );
        })}
      </ol>
      <DemoNote className="text-center text-ink-muted" />
    </figure>
  );
}

/** Répartition des coûts par équipe (réponse à « combien »). */
export function AllocationVisual() {
  return (
    <figure className="rounded-2xl border border-border bg-card p-6 shadow-xl shadow-slate-900/5" aria-label="Répartition de la dépense par équipe">
      <div className="flex items-baseline justify-between">
        <p className="text-sm text-muted">Dépense de septembre</p>
        <p className="text-sm font-medium text-brand">Couverture 96,4 %</p>
      </div>
      <p className="mt-1 text-4xl font-semibold tracking-tight tabular-nums">48 214 €</p>
      <div className="mt-6 space-y-3">
        {teamCosts.map((t) => (
          <div key={t.team}>
            <div className="flex justify-between text-sm">
              <span>{t.team}</span>
              <span className="font-medium tabular-nums">{t.amount}</span>
            </div>
            <div className="mt-1 h-2 overflow-hidden rounded-full bg-bg-2" aria-hidden="true">
              <div className={`fill-bar h-full rounded-full ${t.color}`} style={{ width: `${(t.share / 31) * 100}%` }} />
            </div>
          </div>
        ))}
      </div>
      <DemoNote className="text-subtle" />
    </figure>
  );
}

function UsageBar({ label, requested, used, unit }: { label: string; requested: number; used: number; unit: string }) {
  return (
    <div>
      <div className="flex flex-wrap justify-between gap-x-3 text-sm">
        <span>{label}</span>
        <span className="text-muted tabular-nums">
          <strong className="text-fg">{used.toLocaleString("fr-FR")}</strong> utilisés sur {requested.toLocaleString("fr-FR")} {unit} réservés
        </span>
      </div>
      <div className="relative mt-2 h-3 overflow-hidden rounded-full bg-bg-2 ring-1 ring-border" aria-hidden="true">
        <div className="fill-bar absolute inset-y-0 left-0 rounded-full bg-brand" style={{ width: `${(used / requested) * 100}%` }} />
      </div>
    </div>
  );
}

/** Surdimensionnement et commande prête (réponse à « est-ce bien utilisé »). */
export function RightsizingVisual() {
  return (
    <figure className="rounded-2xl border border-border bg-card p-6 shadow-xl shadow-slate-900/5" aria-label="Exemple de recommandation de dimensionnement">
      <div className="flex items-center justify-between gap-3">
        <p className="font-semibold">
          payment-api <span className="font-normal text-subtle">· 3 réplicas</span>
        </p>
        <span className="rounded-full bg-amber-500/15 px-2.5 py-0.5 text-xs font-medium text-amber-800 dark:text-amber-300">Surdimensionné ×3</span>
      </div>
      <div className="mt-6 space-y-5">
        <UsageBar label="CPU (P95, 30 jours)" requested={4} used={1.2} unit="vCPU" />
        <UsageBar label="Mémoire (P95, 30 jours)" requested={8} used={2.9} unit="Gio" />
      </div>
      <pre className="mt-6 rounded-xl bg-ink p-4 font-mono break-all whitespace-pre-wrap text-[12px] leading-relaxed text-ink-fg">
        <span className="text-ink-muted"># Commande prête à appliquer</span>
        {"\nkubectl -n checkout set resources deploy/payment-api \\\n  --requests=cpu=1500m,memory=3584Mi"}
      </pre>
      <div className="mt-4 flex flex-wrap gap-2 text-xs">
        <span className="rounded-full bg-brand-soft px-2.5 py-1 font-semibold text-brand">−227 €/mois estimés</span>
        <span className="rounded-full bg-bg-2 px-2.5 py-1 text-muted">Risque faible</span>
        <span className="rounded-full bg-bg-2 px-2.5 py-1 text-muted">Économie mesurée après application</span>
      </div>
      <DemoNote className="text-subtle" />
    </figure>
  );
}

// Coût journalier du stockage de l'équipe Data : rupture le 14.
const daily = [312, 318, 309, 321, 316, 324, 319, 322, 317, 326, 321, 318, 325, 441, 452, 448, 455, 450, 458, 454];

/** Variation corrélée à un déploiement (réponse à « pourquoi ça a bougé »). */
export function CorrelationVisual() {
  const w = 480;
  const h = 170;
  const min = 280;
  const max = 480;
  const x = (i: number) => 16 + (i * (w - 32)) / (daily.length - 1);
  const y = (v: number) => h - 20 - ((v - min) / (max - min)) * (h - 40);
  const line = daily.map((v, i) => `${i ? "L" : "M"}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(" ");
  const area = `${line} L${x(daily.length - 1)},${h - 20} L${x(0)},${h - 20} Z`;
  const deploy = x(13) - 0.5 * ((w - 32) / (daily.length - 1));
  return (
    <figure className="rounded-2xl border border-border bg-card p-6 shadow-xl shadow-slate-900/5" aria-label="Anomalie de coût corrélée à un déploiement">
      <div className="flex items-center justify-between gap-3">
        <p className="font-semibold">Stockage · équipe Data</p>
        <span className="flex items-center gap-2 text-xs font-medium text-amber-800 dark:text-amber-300">
          <span className="relative flex size-2">
            <span className="ping absolute inline-flex size-full rounded-full bg-amber-400 opacity-75" />
            <span className="relative inline-flex size-2 rounded-full bg-amber-500" />
          </span>
          Anomalie +38 %
        </span>
      </div>
      <svg viewBox={`0 0 ${w} ${h}`} className="mt-4 w-full" aria-hidden="true">
        <defs>
          <linearGradient id="kairn-area" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" stopColor="#f59e0b" stopOpacity="0.25" />
            <stop offset="1" stopColor="#f59e0b" stopOpacity="0" />
          </linearGradient>
        </defs>
        {[0, 1, 2, 3].map((g) => (
          <line key={g} x1="16" x2={w - 16} y1={20 + g * ((h - 40) / 3)} y2={20 + g * ((h - 40) / 3)} stroke="currentColor" className="text-border" strokeWidth="1" />
        ))}
        <path d={area} fill="url(#kairn-area)" />
        <line x1={deploy} x2={deploy} y1="8" y2={h - 20} stroke="#6366f1" strokeWidth="1.5" strokeDasharray="4 4" />
        <path d={line} pathLength={1} className="draw" fill="none" stroke="#0d9488" strokeWidth="2.5" strokeLinejoin="round" strokeLinecap="round" />
        <circle cx={deploy} cy="8" r="4" fill="#6366f1" />
      </svg>
      <ul className="mt-4 space-y-2 text-sm">
        {[
          ["14 sept. 10:02", "Déploiement etl-v2.3", "Argo CD"],
          ["14 sept. 10:05", "12 volumes de 500 Go créés", "Inventaire"],
          ["15 sept.", "9 volumes détachés, toujours facturés", "Inventaire"],
        ].map(([t, e, s]) => (
          <li key={e} className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span className="w-28 shrink-0 font-mono text-xs text-subtle">{t}</span>
            <span className="font-medium">{e}</span>
            <span className="ml-auto shrink-0 rounded-full bg-bg-2 px-2 py-0.5 text-xs text-muted">{s}</span>
          </li>
        ))}
      </ul>
      <DemoNote className="text-subtle" />
    </figure>
  );
}
