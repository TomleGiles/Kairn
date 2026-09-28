import type { Content } from "@/lib/content";

import { Icon, Logo } from "./icons";
import { RichText } from "./rich";

// Maquette de l'application (HTML/CSS, données fictives de démonstration) :
// légère, nette à toutes les tailles, sans image à charger.
const days = [42, 44, 43, 47, 45, 46, 48, 51, 50, 49, 53, 55, 54, 58, 71, 74, 72, 70, 69, 71];
const split = [0.46, 0.31, 0.23]; // OpenStack, Kubernetes, stockage

const tones = { warn: "text-amber-300", muted: "text-slate-400", good: "text-teal-300" };
const legendColors = ["bg-teal-400", "bg-indigo-400", "bg-sky-400"];

export function ProductMock({ m }: { m: Content["mock"] }) {
  const max = Math.max(...days);
  return (
    <figure className="relative mx-auto w-full max-w-5xl" aria-label={m.aria}>
      <div className="absolute -inset-x-10 -top-10 -bottom-6 -z-10 rounded-[3rem] bg-gradient-to-b from-teal-400/20 via-indigo-500/10 to-transparent blur-3xl" />
      <div className="overflow-hidden rounded-2xl border border-white/10 bg-[#0b1119] shadow-2xl shadow-black/50 ring-1 ring-white/5">
        {/* Barre de fenêtre */}
        <div className="flex items-center gap-2 border-b border-white/10 px-4 py-3">
          <span className="size-3 rounded-full bg-[#ff5f57]" />
          <span className="size-3 rounded-full bg-[#febc2e]" />
          <span className="size-3 rounded-full bg-[#28c840]" />
          <span className="ml-4 hidden rounded-md bg-white/5 px-3 py-1 font-mono text-[11px] text-slate-400 sm:block">{m.windowTitle}</span>
        </div>
        <div className="grid grid-cols-12">
          {/* Barre latérale */}
          <aside className="col-span-3 hidden border-r border-white/10 p-4 text-[12px] text-slate-400 md:block">
            <div className="mb-5 flex items-center gap-2 text-slate-100">
              <Logo className="size-6" />
              <span className="font-semibold">Kairn</span>
            </div>
            {m.sidebar.map((l, i) => (
              <div key={l} className={`mb-1 rounded-md px-2 py-1.5 ${i === 0 ? "bg-teal-400/10 text-teal-300" : ""}`}>
                {l}
              </div>
            ))}
          </aside>
          {/* Contenu */}
          <div className="col-span-12 space-y-4 p-4 sm:p-5 md:col-span-9">
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              {m.kpis.map((k) => (
                <div key={k.label} className="rounded-xl border border-white/10 bg-white/[0.03] p-3">
                  <p className="text-[11px] text-slate-400">{k.label}</p>
                  <p className="mt-1 text-lg font-semibold tabular-nums text-slate-50">{k.value}</p>
                  <p className={`text-[11px] ${tones[k.tone]}`}>{k.sub}</p>
                </div>
              ))}
            </div>
            <div className="grid grid-cols-1 gap-3 lg:grid-cols-5">
              <div className="rounded-xl border border-white/10 bg-white/[0.03] p-4 lg:col-span-3">
                <div className="mb-3 flex items-center justify-between">
                  <p className="text-[12px] font-medium text-slate-200">{m.chartTitle}</p>
                  <p className="text-[11px] text-slate-400">{m.chartRange}</p>
                </div>
                <div className="flex h-36 items-end gap-1.5">
                  {days.map((d, i) => (
                    <div
                      key={i}
                      className="grow-bar flex flex-1 flex-col-reverse overflow-hidden rounded-t-sm"
                      style={{ height: `${(d / max) * 100}%`, "--i": i } as React.CSSProperties}
                    >
                      <div className="bg-teal-400" style={{ height: `${split[0] * 100}%` }} />
                      <div className="bg-indigo-400" style={{ height: `${split[1] * 100}%` }} />
                      <div className={i >= 14 ? "bg-amber-400" : "bg-sky-400"} style={{ height: `${split[2] * 100}%` }} />
                    </div>
                  ))}
                </div>
                <div className="mt-3 flex flex-wrap gap-3 text-[11px] text-slate-400">
                  {m.legend.map((l, i) => (
                    <span key={l} className="flex items-center gap-1.5">
                      <span className={`size-2 rounded-sm ${legendColors[i]}`} />
                      {l}
                    </span>
                  ))}
                </div>
              </div>
              <div className="rounded-xl border border-amber-400/30 bg-amber-400/[0.06] p-4 lg:col-span-2">
                <p className="flex items-center gap-2 text-[12px] font-medium text-amber-200">
                  <Icon name="pulse" className="size-4" /> {m.anomalyTitle}
                </p>
                <p className="mt-2 text-[13px] font-semibold text-slate-50">{m.anomalyHeadline}</p>
                <p className="mt-2 text-[12px] leading-relaxed text-slate-300">
                  <RichText parts={m.anomalyText} codeClassName="font-mono text-teal-300" />
                </p>
                <div className="mt-3 rounded-lg border border-white/10 bg-black/30 p-2.5">
                  <p className="text-[11px] text-slate-400">{m.recoLabel}</p>
                  <p className="text-[12px] text-slate-100">
                    {m.recoText} · <span className="text-teal-300">{m.recoSaving}</span>
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
      {/* Pastilles flottantes : ce que Kairn fait remonter de lui-même. */}
      <div className="float absolute top-24 -left-6 hidden rounded-xl border border-white/10 bg-[#0e1520]/95 p-3 shadow-2xl shadow-black/40 backdrop-blur xl:block" aria-hidden="true">
        <p className="text-[11px] text-slate-400">{m.chipAccepted}</p>
        <p className="text-sm font-semibold text-teal-300">{m.recoSaving}</p>
      </div>
      <div className="float-delayed absolute -right-8 bottom-24 hidden rounded-xl border border-white/10 bg-[#0e1520]/95 p-3 shadow-2xl shadow-black/40 backdrop-blur xl:block" aria-hidden="true">
        <p className="text-[11px] text-slate-400">{m.chipCorrelated}</p>
        <p className="text-sm font-semibold text-indigo-300">Argo CD · etl-v2.3</p>
      </div>
      <figcaption className="mt-3 text-center text-xs text-ink-muted">{m.caption}</figcaption>
    </figure>
  );
}
