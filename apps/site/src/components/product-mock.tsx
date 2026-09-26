import { Icon, Logo } from "./icons";

// Maquette de l'application (HTML/CSS, données fictives de démonstration) :
// légère, nette à toutes les tailles, sans image à charger.
const days = [42, 44, 43, 47, 45, 46, 48, 51, 50, 49, 53, 55, 54, 58, 71, 74, 72, 70, 69, 71];
const split = [0.46, 0.31, 0.23]; // OpenStack, Kubernetes, stockage

export function ProductMock() {
  const max = Math.max(...days);
  return (
    <figure className="relative mx-auto w-full max-w-5xl" aria-label="Aperçu de l'interface Kairn avec des données de démonstration">
      <div className="absolute -inset-x-10 -top-10 -bottom-6 -z-10 rounded-[3rem] bg-gradient-to-b from-teal-400/20 via-indigo-500/10 to-transparent blur-3xl" />
      <div className="overflow-hidden rounded-2xl border border-white/10 bg-[#0b1119] shadow-2xl shadow-black/50 ring-1 ring-white/5">
        {/* Barre de fenêtre */}
        <div className="flex items-center gap-2 border-b border-white/10 px-4 py-3">
          <span className="size-3 rounded-full bg-[#ff5f57]" />
          <span className="size-3 rounded-full bg-[#febc2e]" />
          <span className="size-3 rounded-full bg-[#28c840]" />
          <span className="ml-4 hidden rounded-md bg-white/5 px-3 py-1 font-mono text-[11px] text-slate-400 sm:block">app.kairn · Acme Retail · Vue d&apos;ensemble</span>
        </div>
        <div className="grid grid-cols-12">
          {/* Barre latérale */}
          <aside className="col-span-3 hidden border-r border-white/10 p-4 text-[12px] text-slate-400 md:block">
            <div className="mb-5 flex items-center gap-2 text-slate-100">
              <Logo className="size-6" />
              <span className="font-semibold">Kairn</span>
            </div>
            {["Vue d'ensemble", "Explorateur de coûts", "Coût × usage", "Allocation", "Recommandations", "Anomalies", "Budgets", "Assistant IA"].map((l, i) => (
              <div key={l} className={`mb-1 rounded-md px-2 py-1.5 ${i === 0 ? "bg-teal-400/10 text-teal-300" : ""}`}>
                {l}
              </div>
            ))}
          </aside>
          {/* Contenu */}
          <div className="col-span-12 space-y-4 p-4 sm:p-5 md:col-span-9">
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              {[
                ["Dépense du mois", "48 214 €", "+6,2 %", "text-amber-300"],
                ["Prévision fin de mois", "61 900 €", "budget 64 000 €", "text-slate-400"],
                ["Économies possibles", "7 420 €/mois", "18 recommandations", "text-teal-300"],
                ["Coûts alloués", "96,4 %", "objectif > 95 %", "text-teal-300"],
              ].map(([k, v, s, c]) => (
                <div key={k} className="rounded-xl border border-white/10 bg-white/[0.03] p-3">
                  <p className="text-[11px] text-slate-400">{k}</p>
                  <p className="mt-1 text-lg font-semibold tabular-nums text-slate-50">{v}</p>
                  <p className={`text-[11px] ${c}`}>{s}</p>
                </div>
              ))}
            </div>
            <div className="grid grid-cols-1 gap-3 lg:grid-cols-5">
              <div className="rounded-xl border border-white/10 bg-white/[0.03] p-4 lg:col-span-3">
                <div className="mb-3 flex items-center justify-between">
                  <p className="text-[12px] font-medium text-slate-200">Coût journalier par fournisseur</p>
                  <p className="text-[11px] text-slate-400">20 derniers jours</p>
                </div>
                <div className="flex h-36 items-end gap-1.5">
                  {days.map((d, i) => (
                    <div key={i} className="flex flex-1 flex-col-reverse overflow-hidden rounded-t-sm" style={{ height: `${(d / max) * 100}%` }}>
                      <div className="bg-teal-400" style={{ height: `${split[0] * 100}%` }} />
                      <div className="bg-indigo-400" style={{ height: `${split[1] * 100}%` }} />
                      <div className={i >= 14 ? "bg-amber-400" : "bg-sky-400"} style={{ height: `${split[2] * 100}%` }} />
                    </div>
                  ))}
                </div>
                <div className="mt-3 flex flex-wrap gap-3 text-[11px] text-slate-400">
                  <span className="flex items-center gap-1.5"><span className="size-2 rounded-sm bg-teal-400" />OVHcloud</span>
                  <span className="flex items-center gap-1.5"><span className="size-2 rounded-sm bg-indigo-400" />Kubernetes</span>
                  <span className="flex items-center gap-1.5"><span className="size-2 rounded-sm bg-sky-400" />Stockage</span>
                </div>
              </div>
              <div className="rounded-xl border border-amber-400/30 bg-amber-400/[0.06] p-4 lg:col-span-2">
                <p className="flex items-center gap-2 text-[12px] font-medium text-amber-200">
                  <Icon name="pulse" className="size-4" /> Anomalie détectée
                </p>
                <p className="mt-2 text-[13px] font-semibold text-slate-50">Stockage de l&apos;équipe Data : +38 % depuis le 14</p>
                <p className="mt-2 text-[12px] leading-relaxed text-slate-300">
                  Coïncide avec le déploiement <span className="font-mono text-teal-300">etl-v2.3</span> (Argo CD) : 12 volumes de 500 Go créés, dont 9 non attachés.
                </p>
                <div className="mt-3 rounded-lg border border-white/10 bg-black/30 p-2.5">
                  <p className="text-[11px] text-slate-400">Recommandation</p>
                  <p className="text-[12px] text-slate-100">Supprimer 9 volumes orphelins · <span className="text-teal-300">−612 €/mois</span></p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
      <figcaption className="mt-3 text-center text-xs text-ink-muted">Aperçu de l&apos;interface — données de démonstration.</figcaption>
    </figure>
  );
}
