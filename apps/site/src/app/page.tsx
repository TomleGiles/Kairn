import { Icon, Logo } from "@/components/icons";
import { ProductMock } from "@/components/product-mock";
import { AllocationVisual, CorrelationVisual, Drilldown, InvoiceCompare, RightsizingVisual } from "@/components/visuals";
import { faq, features, integrations, personas, plans, questions, security, sources, sovereignty, steps } from "@/lib/content";
import { demoHref, docsHref, site } from "@/lib/site";

const nav = [
  { href: "#cascade", label: "Comment ça marche" },
  { href: "#produit", label: "Produit" },
  { href: "#ia", label: "IA" },
  { href: "#souverainete", label: "Souveraineté" },
  { href: "#tarifs", label: "Tarifs" },
  { href: "#faq", label: "FAQ" },
];

function jsonLd() {
  return [
    {
      "@context": "https://schema.org",
      "@type": "SoftwareApplication",
      name: site.name,
      url: site.url,
      applicationCategory: "BusinessApplication",
      applicationSubCategory: "FinOps, observabilité des coûts cloud",
      operatingSystem: "Web, Kubernetes (self-hosted)",
      description: site.description,
      inLanguage: "fr",
      offers: { "@type": "AggregateOffer", priceCurrency: "EUR", lowPrice: "99", offerCount: plans.length },
      featureList: features.map((f) => f.title),
    },
    {
      "@context": "https://schema.org",
      "@type": "FAQPage",
      mainEntity: faq.map((f) => ({ "@type": "Question", name: f.q, acceptedAnswer: { "@type": "Answer", text: f.a } })),
    },
  ];
}

function SectionTitle({ id, eyebrow, title, text, dark = false }: { id: string; eyebrow: string; title: React.ReactNode; text?: string; dark?: boolean }) {
  return (
    <div className="reveal mx-auto max-w-3xl text-center">
      <p className={`text-sm font-semibold tracking-widest uppercase ${dark ? "text-teal-300" : "text-brand"}`}>{eyebrow}</p>
      <h2 id={id} className={`mt-4 text-4xl font-semibold tracking-tight text-balance sm:text-5xl ${dark ? "text-ink-fg" : "text-fg"}`}>
        {title}
      </h2>
      {text ? <p className={`mx-auto mt-5 max-w-2xl text-lg text-pretty ${dark ? "text-ink-muted" : "text-muted"}`}>{text}</p> : null}
    </div>
  );
}

function PrimaryButton({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a
      href={href}
      className="group inline-flex items-center gap-2 rounded-full bg-teal-400 px-6 py-3 text-sm font-semibold text-[#04201c] shadow-lg shadow-teal-500/25 transition hover:bg-teal-300 hover:shadow-teal-400/40 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-300"
    >
      {children}
      <Icon name="arrow" className="size-4 transition group-hover:translate-x-0.5" />
    </a>
  );
}

// Les trois questions, chacune illustrée : le texte vient de lib/content.ts.
const chapters = [
  { visual: <AllocationVisual />, bullets: ["Grilles publiques versionnées et factures réelles", "Hiérarchie équipe → service → environnement", "Chargeback exportable en CSV ou par API"] },
  { visual: <RightsizingVisual />, bullets: ["Usage réel au 95ᵉ centile, sur fenêtre glissante", "Orphelins et environnements allumés la nuit", "Commande kubectl, OpenStack ou Terraform fournie"] },
  { visual: <CorrelationVisual />, bullets: ["Baseline saisonnière par série de coûts", "Déploiements, HPA, incidents et inventaire croisés", "Explication générée, preuves à l'appui"] },
];

export default function Home() {
  return (
    <>
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd()) }} />
      <div className="scroll-progress" aria-hidden="true" />

      {/* ------------------------------------------------------------ en-tête */}
      <header className="sticky top-0 z-40 border-b border-white/10 bg-ink/90 text-ink-fg backdrop-blur-xl">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-6 px-4 sm:px-6">
          <a href="#" className="flex items-center gap-2.5 font-semibold" aria-label="Kairn, accueil">
            <Logo className="size-8" />
            <span className="text-lg">Kairn</span>
          </a>
          <nav aria-label="Navigation principale" className="hidden lg:block">
            <ul className="flex gap-6 text-sm text-ink-muted">
              {nav.map((n) => (
                <li key={n.href}>
                  <a className="transition hover:text-ink-fg" href={n.href}>
                    {n.label}
                  </a>
                </li>
              ))}
            </ul>
          </nav>
          <div className="ml-auto flex items-center gap-3">
            <a href={`${site.appUrl}/login`} className="hidden text-sm text-ink-muted hover:text-ink-fg sm:block">
              Se connecter
            </a>
            <a href={demoHref} className="rounded-full bg-white px-4 py-2 text-sm font-semibold text-ink transition hover:bg-slate-200">
              Demander une démo
            </a>
            <details className="relative lg:hidden">
              <summary className="cursor-pointer rounded-md p-2 text-ink-muted hover:text-ink-fg" aria-label="Menu">
                <Icon name="menu" />
              </summary>
              <ul className="absolute right-0 mt-2 w-56 rounded-xl border border-white/10 bg-ink-2 p-2 text-sm shadow-xl">
                {[...nav, { href: docsHref, label: "Documentation" }, { href: `${site.appUrl}/login`, label: "Se connecter" }].map((n) => (
                  <li key={n.href}>
                    <a className="block rounded-md px-3 py-2 text-ink-muted hover:bg-white/5 hover:text-ink-fg" href={n.href}>
                      {n.label}
                    </a>
                  </li>
                ))}
              </ul>
            </details>
          </div>
        </div>
      </header>

      <main id="contenu">
        {/* ------------------------------------------------------------ héros */}
        <section className="relative -mt-16 overflow-hidden bg-ink pt-16 pb-24 text-ink-fg" aria-labelledby="hero-title">
          <div className="grid-bg absolute inset-0" aria-hidden="true" />
          <div className="glow absolute inset-0" aria-hidden="true" />
          <div className="relative mx-auto max-w-7xl px-4 pt-20 sm:px-6 sm:pt-28">
            <div className="mx-auto max-w-4xl text-center">
              <p className="hero-in inline-flex items-center gap-2 rounded-full border border-teal-300/30 bg-teal-300/10 px-3 py-1 text-xs font-medium text-teal-200">
                <Icon name="spark" className="size-3.5" /> FinOps souverain · hébergé dans l&apos;UE ou chez vous
              </p>
              <h1 id="hero-title" className="hero-in mt-7 text-5xl font-semibold tracking-tight text-balance sm:text-7xl" style={{ "--d": 1 } as React.CSSProperties}>
                Le coût réel de votre cloud, <span className="hero-title-glow">enfin expliqué.</span>
              </h1>
              <p className="hero-in mx-auto mt-7 max-w-2xl text-lg text-pretty text-ink-muted sm:text-xl" style={{ "--d": 2 } as React.CSSProperties}>
                De la facture OVHcloud, Scaleway, OUTSCALE ou OpenStack jusqu&apos;au pod Kubernetes : vous savez combien vous dépensez, si c&apos;est bien
                utilisé — et pourquoi ça a bougé.
              </p>
              <div className="hero-in mt-10 flex flex-wrap items-center justify-center gap-4" style={{ "--d": 3 } as React.CSSProperties}>
                <PrimaryButton href={demoHref}>Demander une démo</PrimaryButton>
                <a href="#cascade" className="rounded-full px-6 py-3 text-sm font-semibold text-ink-fg ring-1 ring-white/20 transition hover:bg-white/5">
                  Voir comment ça marche
                </a>
              </div>
              <ul className="hero-in mt-8 flex flex-wrap justify-center gap-x-6 gap-y-2 text-sm text-ink-muted" style={{ "--d": 4 } as React.CSSProperties}>
                {["Connecteurs en lecture seule", "Premiers coûts en moins d'une heure", "Édition self-hosted complète"].map((t) => (
                  <li key={t} className="flex items-center gap-1.5">
                    <Icon name="check" className="size-4 text-teal-300" />
                    {t}
                  </li>
                ))}
              </ul>
            </div>
            <div className="hero-in mt-16 sm:mt-20" style={{ "--d": 4 } as React.CSSProperties}>
              <ProductMock />
            </div>
          </div>
          <a
            href="#probleme"
            className="relative mx-auto mt-14 flex w-fit flex-col items-center gap-2 text-xs text-ink-muted transition hover:text-ink-fg"
          >
            Faites défiler
            <Icon name="chevron" className="float size-5" />
          </a>
        </section>

        {/* ------------------------------------------------------------ sources */}
        <section aria-labelledby="sources-title" className="border-b border-border bg-bg-2 py-10">
          <h2 id="sources-title" className="px-4 text-center text-sm font-medium text-muted">
            Branché sur ce que vous utilisez déjà, sans nouvelle pile de supervision
          </h2>
          <div className="marquee mt-6">
            <ul className="marquee-track gap-x-12 gap-y-4 px-6">
              {sources.map((s) => (
                <li key={s} className="text-lg font-semibold tracking-tight whitespace-nowrap text-subtle">
                  {s}
                </li>
              ))}
              <li className="marquee-copy" aria-hidden="true">
                {sources.map((s) => (
                  <span key={s} className="text-lg font-semibold tracking-tight whitespace-nowrap text-subtle">
                    {s}
                  </span>
                ))}
              </li>
            </ul>
          </div>
        </section>

        {/* ------------------------------------------------------------ le problème */}
        <section id="probleme" className="py-28" aria-labelledby="probleme-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <SectionTitle
              id="probleme-title"
              eyebrow="Le problème"
              title={
                <>
                  Votre facture vous dit combien.
                  <br />
                  <span className="text-brand">Jamais qui, ni pourquoi.</span>
                </>
              }
              text="Les outils FinOps du marché sont pensés pour AWS, Azure et GCP. Sur les clouds souverains, OpenStack et Kubernetes, les équipes finissent dans un tableur. Kairn transforme la même facture en réponses."
            />
            <div className="mt-16">
              <InvoiceCompare />
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ descente facture → pod */}
        <section id="cascade" className="relative overflow-hidden bg-ink py-28 text-ink-fg" aria-labelledby="cascade-title">
          <div className="grid-bg absolute inset-0 opacity-60" aria-hidden="true" />
          <div className="glow absolute inset-0 opacity-70" aria-hidden="true" />
          <div className="relative mx-auto max-w-7xl px-4 sm:px-6">
            <SectionTitle
              id="cascade-title"
              dark
              eyebrow="Comment ça marche"
              title={
                <>
                  De la facture jusqu&apos;au pod.
                  <br />
                  <span className="text-gradient">Puis jusqu&apos;à l&apos;équipe qui paie.</span>
                </>
              }
              text="Kairn relie la facture, les projets cloud, les VM, les nodes Kubernetes et les pods qui tournent dessus. Chaque euro descend la chaîne, avec sa source."
            />
            <div className="mt-20">
              <Drilldown />
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ trois questions */}
        <section id="produit" className="py-28" aria-labelledby="produit-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <SectionTitle
              id="produit-title"
              eyebrow="Produit"
              title="Trois questions. Une réponse chiffrée pour chaque équipe."
              text="Chaque montant est traçable jusqu'à sa source : ligne de facture, grille tarifaire versionnée ou métrique."
            />
            <div className="mt-20 space-y-28">
              {questions.map((q, i) => (
                <article key={q.kicker} className="grid grid-cols-1 items-center gap-12 lg:grid-cols-2 lg:gap-20">
                  <div className={`reveal ${i % 2 ? "lg:order-2" : ""}`}>
                    <p className="flex items-center gap-3 text-sm font-semibold text-brand">
                      <span className="inline-flex size-9 items-center justify-center rounded-full bg-brand-soft font-mono">0{i + 1}</span>
                      {q.kicker}
                    </p>
                    <h3 className="mt-5 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">{q.title}</h3>
                    <p className="mt-5 text-lg leading-relaxed text-pretty text-muted">{q.text}</p>
                    <ul className="mt-7 space-y-3">
                      {chapters[i].bullets.map((b) => (
                        <li key={b} className="flex gap-3">
                          <Icon name="check" className="mt-0.5 size-5 shrink-0 text-brand" />
                          <span>{b}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                  <div className={`reveal-scale ${i % 2 ? "lg:order-1" : ""}`}>{chapters[i].visual}</div>
                </article>
              ))}
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ fonctionnalités */}
        <section className="border-y border-border bg-bg-2 py-28" aria-labelledby="fonctionnalites-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <SectionTitle id="fonctionnalites-title" eyebrow="Tout le reste" title="De la facture à l'action, sans tableur" />
            <div className="mt-16 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
              {features.map((f, i) => (
                <article
                  key={f.title}
                  className={`reveal group rounded-2xl border border-border bg-card p-6 transition hover:-translate-y-1 hover:border-brand/40 hover:shadow-xl hover:shadow-teal-900/10 ${i < 2 || i >= features.length - 2 ? "lg:col-span-2" : ""}`}
                >
                  <span className="inline-flex size-11 items-center justify-center rounded-xl bg-brand-soft text-brand transition group-hover:scale-110">
                    <Icon name={f.icon} />
                  </span>
                  <h3 className={`mt-5 font-semibold tracking-tight ${i < 2 || i >= features.length - 2 ? "text-xl" : ""}`}>{f.title}</h3>
                  <p className="mt-2 text-sm leading-relaxed text-muted">{f.text}</p>
                </article>
              ))}
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ IA */}
        <section id="ia" className="py-28" aria-labelledby="ia-title">
          <div className="mx-auto grid max-w-7xl items-center gap-14 px-4 sm:px-6 lg:grid-cols-2">
            <div className="reveal">
              <p className="text-sm font-semibold tracking-widest text-brand uppercase">Assistant IA</p>
              <h2 id="ia-title" className="mt-4 text-4xl font-semibold tracking-tight text-balance sm:text-5xl">
                Posez la question. Obtenez des chiffres vérifiés.
              </h2>
              <p className="mt-5 text-lg text-pretty text-muted">
                L&apos;assistant interroge vos données à travers des outils typés, avec exactement vos droits. Chaque nombre de sa réponse est contrôlé
                contre les résultats de ces outils, et les sources sont citées.
              </p>
              <ul className="mt-8 space-y-3">
                {[
                  "Anthropic, Mistral (option souveraine) ou modèle local — ou pas d'IA du tout",
                  "Aucun appel hors UE sans l'accord explicite de votre organisation",
                  "Rapport mensuel exécutif rédigé pour la direction, envoyé en PDF",
                  "Serveur MCP : vos propres agents IA interrogent Kairn avec vos droits",
                ].map((t) => (
                  <li key={t} className="flex gap-3 text-muted">
                    <Icon name="check" className="mt-0.5 size-5 shrink-0 text-brand" />
                    <span>{t}</span>
                  </li>
                ))}
              </ul>
            </div>
            <figure
              className="reveal-scale relative rounded-3xl border border-border bg-card p-6 shadow-2xl shadow-slate-900/10"
              aria-label="Exemple de conversation avec l'assistant Kairn"
            >
              <div className="absolute -inset-4 -z-10 rounded-[2rem] bg-gradient-to-br from-teal-400/20 via-transparent to-indigo-500/20 blur-2xl" aria-hidden="true" />
              <div className="reveal flex justify-end">
                <p className="max-w-[85%] rounded-2xl rounded-br-md bg-brand px-4 py-2.5 text-sm text-pretty text-brand-fg">
                  Pourquoi les coûts de l&apos;équipe Data ont-ils augmenté ce mois-ci ?
                </p>
              </div>
              <div className="reveal reveal-late mt-5 flex gap-3">
                <span className="inline-flex size-8 shrink-0 items-center justify-center rounded-full bg-brand-soft text-brand">
                  <Icon name="spark" className="size-4" />
                </span>
                <div className="space-y-3 text-sm leading-relaxed">
                  <p>
                    L&apos;équipe Data a dépensé <strong>14 950 €</strong> en septembre, soit <strong>+3 290 €</strong> (+28,2 %) par rapport à août.
                  </p>
                  <p>
                    La hausse vient surtout du stockage : <strong>+2 480 €</strong> après le déploiement{" "}
                    <code className="rounded bg-bg-2 px-1 font-mono text-xs">etl-v2.3</code> du 14, qui a créé 12 volumes dont 9 ne sont plus attachés.
                  </p>
                  <p className="rounded-lg border border-border bg-bg-2 p-3 text-muted">
                    Action suggérée : supprimer les 9 volumes orphelins (<strong className="text-fg">−612 €/mois</strong>). Commande prête dans la recommandation.
                  </p>
                  <p className="text-xs text-subtle">Sources : coûts par équipe, événements Argo CD, inventaire des volumes.</p>
                </div>
              </div>
              <figcaption className="mt-5 border-t border-border pt-3 text-xs text-subtle">Exemple illustratif, données de démonstration.</figcaption>
            </figure>
          </div>
        </section>

        {/* ------------------------------------------------------------ personas */}
        <section className="border-y border-border bg-bg-2 py-28" aria-labelledby="equipes-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <SectionTitle id="equipes-title" eyebrow="Pour qui" title="Une seule source de vérité, de l'ingénieur à la direction financière" />
            <div className="mt-16 grid gap-5 sm:grid-cols-2 lg:grid-cols-4">
              {personas.map((p) => (
                <article key={p.role} className="reveal flex flex-col rounded-2xl border border-border bg-card p-6 transition hover:-translate-y-1 hover:shadow-xl hover:shadow-slate-900/5">
                  <h3 className="text-lg font-semibold tracking-tight">{p.role}</h3>
                  <p className="mt-2 text-sm text-muted">{p.need}</p>
                  <ul className="mt-5 space-y-2 border-t border-border pt-5 text-sm">
                    {p.points.map((pt) => (
                      <li key={pt} className="flex gap-2">
                        <Icon name="check" className="mt-0.5 size-4 shrink-0 text-brand" />
                        {pt}
                      </li>
                    ))}
                  </ul>
                </article>
              ))}
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ démarrage */}
        <section className="py-28" aria-labelledby="demarrage-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <SectionTitle id="demarrage-title" eyebrow="Mise en route" title="Opérationnel en une journée, pas en un trimestre" />
            <ol className="relative mt-16 grid gap-6 md:grid-cols-3">
              {steps.map((s) => (
                <li key={s.n} className="reveal relative rounded-2xl border border-border bg-card p-7 text-center">
                  <span className="relative mx-auto inline-flex size-14 items-center justify-center rounded-full bg-brand font-mono text-lg font-semibold text-brand-fg shadow-lg shadow-teal-500/30">
                    {s.n}
                  </span>
                  <h3 className="mt-5 text-xl font-semibold tracking-tight">{s.title}</h3>
                  <p className="mt-3 leading-relaxed text-muted">{s.text}</p>
                </li>
              ))}
            </ol>
          </div>
        </section>

        {/* ------------------------------------------------------------ souveraineté */}
        <section id="souverainete" className="relative overflow-hidden bg-ink py-28 text-ink-fg" aria-labelledby="souverainete-title">
          <div className="glow absolute inset-0 opacity-60" aria-hidden="true" />
          <div className="relative mx-auto max-w-7xl px-4 sm:px-6">
            <SectionTitle
              id="souverainete-title"
              dark
              eyebrow="Souveraineté"
              title={
                <>
                  Vos données de coûts restent en Europe. <span className="text-gradient">Ou chez vous.</span>
                </>
              }
              text="Vos coûts révèlent votre architecture, vos volumes et vos clients. Kairn est conçu pour que vous en gardiez la maîtrise."
            />
            <div className="mt-16 grid gap-5 sm:grid-cols-2 lg:grid-cols-4">
              {sovereignty.map((s) => (
                <article key={s.title} className="reveal rounded-2xl border border-ink-border bg-ink-2/80 p-6 transition hover:border-teal-300/40">
                  <span className="inline-flex size-11 items-center justify-center rounded-xl bg-teal-300/10 text-teal-300">
                    <Icon name={s.icon} />
                  </span>
                  <h3 className="mt-5 font-semibold tracking-tight">{s.title}</h3>
                  <p className="mt-2 text-sm leading-relaxed text-ink-muted">{s.text}</p>
                </article>
              ))}
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ sécurité et intégrations */}
        <section className="py-28" aria-labelledby="securite-title">
          <div className="mx-auto grid max-w-7xl gap-14 px-4 sm:px-6 lg:grid-cols-2">
            <div className="reveal">
              <p className="text-sm font-semibold tracking-widest text-brand uppercase">Sécurité</p>
              <h2 id="securite-title" className="mt-4 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
                Conçu pour les exigences des grands comptes et du secteur public
              </h2>
              <ul className="mt-8 space-y-4">
                {security.map((s) => (
                  <li key={s} className="flex gap-3">
                    <Icon name="shield" className="mt-0.5 size-5 shrink-0 text-brand" />
                    <span className="text-muted">{s}</span>
                  </li>
                ))}
              </ul>
            </div>
            <div className="reveal">
              <p className="text-sm font-semibold tracking-widest text-brand uppercase">Intégrations</p>
              <h2 className="mt-4 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">Dans vos outils et vos pipelines</h2>
              <div className="mt-8 grid gap-4 sm:grid-cols-2">
                {integrations.map((i) => (
                  <article key={i.title} className="rounded-2xl border border-border bg-card p-5 transition hover:border-brand/40">
                    <Icon name={i.icon} className="size-5 text-brand" />
                    <h3 className="mt-3 font-semibold">{i.title}</h3>
                    <p className="mt-1 text-sm text-muted">{i.text}</p>
                  </article>
                ))}
              </div>
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ tarifs */}
        <section id="tarifs" className="border-y border-border bg-bg-2 py-28" aria-labelledby="tarifs-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <SectionTitle
              id="tarifs-title"
              eyebrow="Tarifs"
              title="Un prix aligné sur la valeur, pas sur le nombre de ressources"
              text="Prix indicatifs hors taxes. Les limites de chaque plan sont appliquées par la plateforme."
            />
            <div className="mt-16 grid gap-6 md:grid-cols-2 xl:grid-cols-4">
              {plans.map((p) => (
                <article
                  key={p.name}
                  className={`reveal relative flex flex-col rounded-2xl border bg-card p-7 transition hover:-translate-y-1 ${p.highlight ? "border-brand shadow-2xl shadow-teal-900/15 ring-1 ring-brand xl:scale-105" : "border-border hover:shadow-xl hover:shadow-slate-900/5"}`}
                >
                  {p.highlight ? (
                    <p className="absolute -top-3 left-7 rounded-full bg-brand px-3 py-0.5 text-xs font-semibold text-brand-fg">Le plus complet</p>
                  ) : null}
                  <h3 className="text-lg font-semibold">{p.name}</h3>
                  <p className="text-sm text-muted">{p.target}</p>
                  <p className="mt-6">
                    <span className="text-4xl font-semibold tracking-tight">{p.price}</span> <span className="text-sm text-muted">{p.unit}</span>
                  </p>
                  <ul className="mt-6 flex-1 space-y-2.5 text-sm">
                    {p.features.map((f) => (
                      <li key={f} className="flex gap-2">
                        <Icon name="check" className="mt-0.5 size-4 shrink-0 text-brand" />
                        {f}
                      </li>
                    ))}
                  </ul>
                  <a
                    href={demoHref}
                    className={`mt-8 rounded-full px-4 py-2.5 text-center text-sm font-semibold transition ${p.highlight ? "bg-brand text-brand-fg hover:bg-brand-strong" : "ring-1 ring-border hover:bg-bg-2"}`}
                  >
                    {p.price === "Sur devis" ? "Nous contacter" : "Demander une démo"}
                    <span className="sr-only"> — plan {p.name}</span>
                  </a>
                </article>
              ))}
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ FAQ */}
        <section id="faq" className="py-28" aria-labelledby="faq-title">
          <div className="mx-auto max-w-3xl px-4 sm:px-6">
            <h2 id="faq-title" className="reveal text-center text-4xl font-semibold tracking-tight">
              Questions fréquentes
            </h2>
            <div className="reveal mt-12 divide-y divide-border rounded-2xl border border-border bg-card">
              {faq.map((f) => (
                <details key={f.q} className="group px-6 py-5">
                  <summary className="flex cursor-pointer items-center justify-between gap-4 font-medium">
                    <h3>{f.q}</h3>
                    <Icon name="chevron" className="chevron size-5 shrink-0 text-subtle transition" />
                  </summary>
                  <p className="mt-3 leading-relaxed text-muted">{f.a}</p>
                </details>
              ))}
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ appel final */}
        <section className="px-4 pb-28 sm:px-6" aria-labelledby="cta-title">
          <div className="reveal-scale relative mx-auto max-w-7xl overflow-hidden rounded-[2rem] bg-ink px-6 py-20 text-center text-ink-fg sm:px-16 sm:py-24">
            <div className="grid-bg absolute inset-0" aria-hidden="true" />
            <div className="glow absolute inset-0" aria-hidden="true" />
            <div className="relative">
              <h2 id="cta-title" className="text-4xl font-semibold tracking-tight text-balance sm:text-6xl">
                Voyez vos propres coûts, <span className="hero-title-glow">expliqués.</span>
              </h2>
              <p className="mx-auto mt-6 max-w-xl text-lg text-ink-muted">
                Une démonstration de 30 minutes sur votre contexte : clouds, Kubernetes, organisation des équipes et contraintes de souveraineté.
              </p>
              <div className="mt-10 flex flex-wrap justify-center gap-4">
                <PrimaryButton href={demoHref}>Demander une démo</PrimaryButton>
                <a href={docsHref} className="rounded-full px-6 py-3 text-sm font-semibold ring-1 ring-white/20 transition hover:bg-white/5">
                  Explorer la documentation
                </a>
              </div>
            </div>
          </div>
        </section>
      </main>

      {/* ------------------------------------------------------------ pied de page */}
      <footer className="border-t border-border bg-bg-2">
        <div className="mx-auto flex max-w-7xl flex-col gap-8 px-4 py-12 sm:px-6 md:flex-row md:justify-between">
          <div className="max-w-sm">
            <div className="flex items-center gap-2.5 font-semibold">
              <Logo className="size-7" />
              Kairn
            </div>
            <p className="mt-3 text-sm text-muted">Observabilité des coûts pour les clouds souverains, OpenStack et Kubernetes.</p>
          </div>
          <nav aria-label="Pied de page" className="grid grid-cols-2 gap-8 text-sm sm:grid-cols-3">
            <div>
              <p className="font-semibold">Produit</p>
              <ul className="mt-3 space-y-2 text-muted">
                <li><a className="hover:text-fg" href="#cascade">Comment ça marche</a></li>
                <li><a className="hover:text-fg" href="#produit">Fonctionnalités</a></li>
                <li><a className="hover:text-fg" href="#ia">Assistant IA</a></li>
                <li><a className="hover:text-fg" href="#tarifs">Tarifs</a></li>
              </ul>
            </div>
            <div>
              <p className="font-semibold">Ressources</p>
              <ul className="mt-3 space-y-2 text-muted">
                <li><a className="hover:text-fg" href={docsHref}>Documentation</a></li>
                <li><a className="hover:text-fg" href={`${docsHref}/connectors`}>Connecteurs</a></li>
                <li><a className="hover:text-fg" href={`${docsHref}/guide/securite`}>Sécurité</a></li>
              </ul>
            </div>
            <div>
              <p className="font-semibold">Contact</p>
              <ul className="mt-3 space-y-2 text-muted">
                <li><a className="hover:text-fg" href={demoHref}>Demander une démo</a></li>
                <li><a className="break-all hover:text-fg" href={`mailto:${site.contactEmail}`}>{site.contactEmail}</a></li>
              </ul>
            </div>
          </nav>
        </div>
        <p className="border-t border-border py-6 text-center text-xs text-subtle">© {new Date().getFullYear()} Kairn. Tous droits réservés.</p>
      </footer>
    </>
  );
}
