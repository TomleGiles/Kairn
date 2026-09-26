import { Icon, Logo } from "@/components/icons";
import { ProductMock } from "@/components/product-mock";
import { faq, features, integrations, personas, plans, questions, security, sources, sovereignty, steps } from "@/lib/content";
import { demoHref, docsHref, site } from "@/lib/site";

const nav = [
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

function SectionTitle({ id, eyebrow, title, text, dark = false }: { id: string; eyebrow: string; title: string; text?: string; dark?: boolean }) {
  return (
    <div className="mx-auto max-w-2xl text-center">
      <p className={`text-sm font-semibold tracking-wide uppercase ${dark ? "text-teal-300" : "text-brand"}`}>{eyebrow}</p>
      <h2 id={id} className={`mt-3 text-3xl font-semibold tracking-tight text-balance sm:text-4xl ${dark ? "text-ink-fg" : "text-fg"}`}>{title}</h2>
      {text ? <p className={`mt-4 text-lg text-pretty ${dark ? "text-ink-muted" : "text-muted"}`}>{text}</p> : null}
    </div>
  );
}

function PrimaryButton({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a
      href={href}
      className="inline-flex items-center gap-2 rounded-full bg-teal-400 px-5 py-2.5 text-sm font-semibold text-[#04201c] shadow-lg shadow-teal-500/20 transition hover:bg-teal-300 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-300"
    >
      {children}
      <Icon name="arrow" className="size-4" />
    </a>
  );
}

export default function Home() {
  return (
    <>
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd()) }} />

      {/* ------------------------------------------------------------ en-tête */}
      <header className="sticky top-0 z-40 border-b border-white/10 bg-ink text-ink-fg">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-6 px-4 sm:px-6">
          <a href="#" className="flex items-center gap-2.5 font-semibold" aria-label="Kairn, accueil">
            <Logo className="size-8" />
            <span className="text-lg">Kairn</span>
          </a>
          <nav aria-label="Navigation principale" className="hidden md:block">
            <ul className="flex gap-6 text-sm text-ink-muted">
              {nav.map((n) => (
                <li key={n.href}>
                  <a className="hover:text-ink-fg" href={n.href}>
                    {n.label}
                  </a>
                </li>
              ))}
              <li>
                <a className="hover:text-ink-fg" href={docsHref}>
                  Documentation
                </a>
              </li>
            </ul>
          </nav>
          <div className="ml-auto flex items-center gap-3">
            <a href={`${site.appUrl}/login`} className="hidden text-sm text-ink-muted hover:text-ink-fg sm:block">
              Se connecter
            </a>
            <a href={demoHref} className="rounded-full bg-white px-4 py-2 text-sm font-semibold text-ink hover:bg-slate-200">
              Demander une démo
            </a>
            <details className="relative md:hidden">
              <summary className="cursor-pointer rounded-md p-2 text-ink-muted hover:text-ink-fg" aria-label="Menu">
                <Icon name="menu" />
              </summary>
              <ul className="absolute right-0 mt-2 w-48 rounded-xl border border-white/10 bg-ink-2 p-2 text-sm shadow-xl">
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
        <section className="relative overflow-hidden bg-ink pb-20 text-ink-fg" aria-labelledby="hero-title">
          <div className="grid-bg absolute inset-0" aria-hidden="true" />
          <div className="glow absolute inset-0" aria-hidden="true" />
          <div className="relative mx-auto max-w-7xl px-4 pt-20 sm:px-6 sm:pt-28">
            <div className="mx-auto max-w-3xl text-center">
              <p className="inline-flex items-center gap-2 rounded-full border border-teal-300/30 bg-teal-300/10 px-3 py-1 text-xs font-medium text-teal-200">
                <Icon name="spark" className="size-3.5" /> FinOps souverain · hébergé dans l&apos;UE ou chez vous
              </p>
              <h1 id="hero-title" className="mt-6 text-4xl font-semibold tracking-tight text-balance sm:text-6xl">
                Le coût réel de votre cloud, <span className="text-gradient">enfin expliqué.</span>
              </h1>
              <p className="mx-auto mt-6 max-w-2xl text-lg text-pretty text-ink-muted">
                Kairn relie coûts, usage et déploiements sur OVHcloud, Scaleway, OUTSCALE, OpenStack et Kubernetes. Vous savez combien vous dépensez, si
                c&apos;est bien utilisé — et pourquoi ça a bougé.
              </p>
              <div className="mt-9 flex flex-wrap items-center justify-center gap-4">
                <PrimaryButton href={demoHref}>Demander une démo</PrimaryButton>
                <a href={docsHref} className="rounded-full px-5 py-2.5 text-sm font-semibold text-ink-fg ring-1 ring-white/20 hover:bg-white/5">
                  Lire la documentation
                </a>
              </div>
              <ul className="mt-8 flex flex-wrap justify-center gap-x-6 gap-y-2 text-sm text-ink-muted">
                {["Connecteurs en lecture seule", "Édition self-hosted complète", "IA souveraine au choix"].map((t) => (
                  <li key={t} className="flex items-center gap-1.5">
                    <Icon name="check" className="size-4 text-teal-300" />
                    {t}
                  </li>
                ))}
              </ul>
            </div>
            <div className="mt-16">
              <ProductMock />
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ sources */}
        <section aria-labelledby="sources-title" className="border-b border-border bg-bg-2 py-10">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <h2 id="sources-title" className="text-center text-sm font-medium text-muted">
              Branché sur ce que vous utilisez déjà, sans nouvelle pile de supervision
            </h2>
            <ul className="mt-6 flex flex-wrap items-center justify-center gap-x-8 gap-y-4">
              {sources.map((s) => (
                <li key={s} className="text-base font-semibold tracking-tight text-subtle">
                  {s}
                </li>
              ))}
            </ul>
          </div>
        </section>

        {/* ------------------------------------------------------------ trois questions */}
        <section className="py-24" aria-labelledby="questions-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <SectionTitle
              id="questions-title"
              eyebrow="Pourquoi Kairn"
              title="Trois questions, une réponse chiffrée pour chaque équipe"
              text="Les outils FinOps du marché sont pensés pour AWS, Azure et GCP. Kairn est conçu pour les clouds souverains, OpenStack et Kubernetes — là où les autres voient mal."
            />
            <div className="mt-14 grid gap-6 md:grid-cols-3">
              {questions.map((q, i) => (
                <article key={q.kicker} className="relative rounded-2xl border border-border bg-card p-7 shadow-sm">
                  <span className="font-mono text-sm text-subtle">0{i + 1}</span>
                  <p className="mt-3 text-sm font-semibold text-brand">{q.kicker}</p>
                  <h3 className="mt-1 text-xl font-semibold tracking-tight">{q.title}</h3>
                  <p className="mt-3 leading-relaxed text-muted">{q.text}</p>
                </article>
              ))}
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ fonctionnalités */}
        <section id="produit" className="border-y border-border bg-bg-2 py-24" aria-labelledby="produit-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <div className="mx-auto max-w-2xl text-center">
              <p className="text-sm font-semibold tracking-wide text-brand uppercase">Produit</p>
              <h2 id="produit-title" className="mt-3 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
                De la facture à l&apos;action, sans tableur
              </h2>
              <p className="mt-4 text-lg text-pretty text-muted">
                Chaque montant est traçable jusqu&apos;à sa source : ligne de facture, grille tarifaire versionnée ou métrique.
              </p>
            </div>
            <div className="mt-14 grid gap-px overflow-hidden rounded-2xl border border-border bg-border sm:grid-cols-2 lg:grid-cols-4">
              {features.map((f) => (
                <article key={f.title} className="bg-card p-6">
                  <span className="inline-flex size-10 items-center justify-center rounded-lg bg-brand-soft text-brand">
                    <Icon name={f.icon} />
                  </span>
                  <h3 className="mt-4 font-semibold tracking-tight">{f.title}</h3>
                  <p className="mt-2 text-sm leading-relaxed text-muted">{f.text}</p>
                </article>
              ))}
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ IA */}
        <section id="ia" className="py-24" aria-labelledby="ia-title">
          <div className="mx-auto grid max-w-7xl items-center gap-12 px-4 sm:px-6 lg:grid-cols-2">
            <div>
              <p className="text-sm font-semibold tracking-wide text-brand uppercase">Assistant IA</p>
              <h2 id="ia-title" className="mt-3 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
                Posez la question. Obtenez des chiffres vérifiés, pas des hallucinations.
              </h2>
              <p className="mt-4 text-lg text-pretty text-muted">
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
            <figure className="rounded-2xl border border-border bg-card p-5 shadow-xl shadow-slate-900/5" aria-label="Exemple de conversation avec l'assistant Kairn">
              <div className="flex justify-end">
                <p className="max-w-[85%] rounded-2xl rounded-br-md bg-brand px-4 py-2.5 text-sm text-brand-fg">
                  Pourquoi les coûts de l&apos;équipe Data ont-ils augmenté ce mois-ci ?
                </p>
              </div>
              <div className="mt-4 flex gap-3">
                <span className="inline-flex size-8 shrink-0 items-center justify-center rounded-full bg-brand-soft text-brand">
                  <Icon name="spark" className="size-4" />
                </span>
                <div className="space-y-3 text-sm leading-relaxed">
                  <p>
                    L&apos;équipe Data a dépensé <strong>18 430 €</strong> en septembre, soit <strong>+4 120 €</strong> (+28,8 %) par rapport à août.
                  </p>
                  <p>
                    La hausse vient surtout du stockage : <strong>+3 050 €</strong> après le déploiement <code className="rounded bg-bg-2 px-1 font-mono text-xs">etl-v2.3</code>{" "}
                    du 14, qui a créé 12 volumes dont 9 ne sont plus attachés.
                  </p>
                  <p className="rounded-lg border border-border bg-bg-2 p-3 text-muted">
                    Action suggérée : supprimer les 9 volumes orphelins (<strong className="text-fg">−612 €/mois</strong>). Commande prête dans la recommandation.
                  </p>
                  <p className="text-xs text-subtle">Sources : coûts par équipe, événements Argo CD, inventaire des volumes.</p>
                </div>
              </div>
              <figcaption className="mt-4 border-t border-border pt-3 text-xs text-subtle">Exemple illustratif, données de démonstration.</figcaption>
            </figure>
          </div>
        </section>

        {/* ------------------------------------------------------------ personas */}
        <section className="border-y border-border bg-bg-2 py-24" aria-labelledby="equipes-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <div className="mx-auto max-w-2xl text-center">
              <p className="text-sm font-semibold tracking-wide text-brand uppercase">Pour qui</p>
              <h2 id="equipes-title" className="mt-3 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
                Une seule source de vérité, de l&apos;ingénieur à la direction financière
              </h2>
            </div>
            <div className="mt-14 grid gap-6 sm:grid-cols-2 lg:grid-cols-4">
              {personas.map((p) => (
                <article key={p.role} className="flex flex-col rounded-2xl border border-border bg-card p-6">
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
        <section className="py-24" aria-labelledby="demarrage-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <div className="mx-auto max-w-2xl text-center">
              <p className="text-sm font-semibold tracking-wide text-brand uppercase">Mise en route</p>
              <h2 id="demarrage-title" className="mt-3 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
                Opérationnel en une journée, pas en un trimestre
              </h2>
            </div>
            <ol className="mt-14 grid gap-6 md:grid-cols-3">
              {steps.map((s) => (
                <li key={s.n} className="rounded-2xl border border-border bg-card p-7">
                  <span className="font-mono text-3xl font-semibold text-brand">{s.n}</span>
                  <h3 className="mt-4 text-xl font-semibold tracking-tight">{s.title}</h3>
                  <p className="mt-3 leading-relaxed text-muted">{s.text}</p>
                </li>
              ))}
            </ol>
          </div>
        </section>

        {/* ------------------------------------------------------------ souveraineté */}
        <section id="souverainete" className="relative overflow-hidden bg-ink py-24 text-ink-fg" aria-labelledby="souverainete-title">
          <div className="glow absolute inset-0 opacity-60" aria-hidden="true" />
          <div className="relative mx-auto max-w-7xl px-4 sm:px-6">
            <div className="mx-auto max-w-2xl text-center">
              <p className="text-sm font-semibold tracking-wide text-teal-300 uppercase">Souveraineté</p>
              <h2 id="souverainete-title" className="mt-3 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
                Vos données de coûts restent en Europe. Ou chez vous.
              </h2>
              <p className="mt-4 text-lg text-pretty text-ink-muted">
                Vos coûts révèlent votre architecture, vos volumes et vos clients. Kairn est conçu pour que vous en gardiez la maîtrise.
              </p>
            </div>
            <div className="mt-14 grid gap-6 sm:grid-cols-2 lg:grid-cols-4">
              {sovereignty.map((s) => (
                <article key={s.title} className="rounded-2xl border border-ink-border bg-ink-2/80 p-6">
                  <span className="inline-flex size-10 items-center justify-center rounded-lg bg-teal-300/10 text-teal-300">
                    <Icon name={s.icon} />
                  </span>
                  <h3 className="mt-4 font-semibold tracking-tight">{s.title}</h3>
                  <p className="mt-2 text-sm leading-relaxed text-ink-muted">{s.text}</p>
                </article>
              ))}
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ sécurité et intégrations */}
        <section className="py-24" aria-labelledby="securite-title">
          <div className="mx-auto grid max-w-7xl gap-12 px-4 sm:px-6 lg:grid-cols-2">
            <div>
              <p className="text-sm font-semibold tracking-wide text-brand uppercase">Sécurité</p>
              <h2 id="securite-title" className="mt-3 text-3xl font-semibold tracking-tight text-balance">
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
            <div>
              <p className="text-sm font-semibold tracking-wide text-brand uppercase">Intégrations</p>
              <h2 className="mt-3 text-3xl font-semibold tracking-tight text-balance">Dans vos outils et vos pipelines</h2>
              <div className="mt-8 grid gap-4 sm:grid-cols-2">
                {integrations.map((i) => (
                  <article key={i.title} className="rounded-2xl border border-border bg-card p-5">
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
        <section id="tarifs" className="border-y border-border bg-bg-2 py-24" aria-labelledby="tarifs-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <div className="mx-auto max-w-2xl text-center">
              <p className="text-sm font-semibold tracking-wide text-brand uppercase">Tarifs</p>
              <h2 id="tarifs-title" className="mt-3 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
                Un prix aligné sur la valeur, pas sur le nombre de ressources
              </h2>
              <p className="mt-4 text-muted">Prix indicatifs hors taxes. Les limites de chaque plan sont appliquées par la plateforme.</p>
            </div>
            <div className="mt-14 grid gap-6 md:grid-cols-2 xl:grid-cols-4">
              {plans.map((p) => (
                <article
                  key={p.name}
                  className={`relative flex flex-col rounded-2xl border bg-card p-7 ${p.highlight ? "border-brand shadow-xl shadow-teal-900/10 ring-1 ring-brand" : "border-border"}`}
                >
                  {p.highlight ? (
                    <p className="absolute -top-3 left-7 rounded-full bg-brand px-3 py-0.5 text-xs font-semibold text-brand-fg">Le plus complet</p>
                  ) : null}
                  <h3 className="text-lg font-semibold">{p.name}</h3>
                  <p className="text-sm text-muted">{p.target}</p>
                  <p className="mt-6">
                    <span className="text-3xl font-semibold tracking-tight">{p.price}</span> <span className="text-sm text-muted">{p.unit}</span>
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
                    className={`mt-8 rounded-full px-4 py-2.5 text-center text-sm font-semibold ${p.highlight ? "bg-brand text-brand-fg hover:bg-brand-strong" : "ring-1 ring-border hover:bg-bg-2"}`}
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
        <section id="faq" className="py-24" aria-labelledby="faq-title">
          <div className="mx-auto max-w-3xl px-4 sm:px-6">
            <h2 id="faq-title" className="text-center text-3xl font-semibold tracking-tight">
              Questions fréquentes
            </h2>
            <div className="mt-12 divide-y divide-border rounded-2xl border border-border bg-card">
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
        <section className="px-4 pb-24 sm:px-6" aria-labelledby="cta-title">
          <div className="relative mx-auto max-w-7xl overflow-hidden rounded-3xl bg-ink px-6 py-16 text-center text-ink-fg sm:px-16">
            <div className="glow absolute inset-0" aria-hidden="true" />
            <div className="relative">
              <h2 id="cta-title" className="text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
                Voyez vos propres coûts, expliqués.
              </h2>
              <p className="mx-auto mt-4 max-w-xl text-lg text-ink-muted">
                Une démonstration de 30 minutes sur votre contexte : clouds, Kubernetes, organisation des équipes et contraintes de souveraineté.
              </p>
              <div className="mt-8 flex flex-wrap justify-center gap-4">
                <PrimaryButton href={demoHref}>Demander une démo</PrimaryButton>
                <a href={docsHref} className="rounded-full px-5 py-2.5 text-sm font-semibold ring-1 ring-white/20 hover:bg-white/5">
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
                <li><a className="hover:text-fg" href={`mailto:${site.contactEmail}`}>{site.contactEmail}</a></li>
              </ul>
            </div>
          </nav>
        </div>
        <p className="border-t border-border py-6 text-center text-xs text-subtle">© {new Date().getFullYear()} Kairn. Tous droits réservés.</p>
      </footer>
    </>
  );
}
