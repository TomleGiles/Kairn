import { Icon, Logo } from "@/components/icons";
import { ProductMock } from "@/components/product-mock";
import { RichText } from "@/components/rich";
import { AllocationVisual, CorrelationVisual, Drilldown, InvoiceCompare, RightsizingVisual } from "@/components/visuals";
import { getContent, homePath, type Content, type Locale } from "@/lib/content";
import { demoHref, docsHref, site } from "@/lib/site";

function jsonLd(c: Content) {
  return [
    {
      "@context": "https://schema.org",
      "@type": "SoftwareApplication",
      name: site.name,
      url: `${site.url}${homePath[c.locale]}`,
      applicationCategory: "BusinessApplication",
      applicationSubCategory: "FinOps",
      operatingSystem: "Web, Kubernetes (self-hosted)",
      description: c.meta.description,
      inLanguage: c.htmlLang,
      offers: { "@type": "AggregateOffer", priceCurrency: "EUR", lowPrice: "99", offerCount: c.plans.length },
      featureList: c.features.map((f) => f.title),
    },
    {
      "@context": "https://schema.org",
      "@type": "FAQPage",
      inLanguage: c.htmlLang,
      mainEntity: c.faq.map((f) => ({ "@type": "Question", name: f.q, acceptedAnswer: { "@type": "Answer", text: f.a } })),
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

function LocaleSwitch({ c, className = "" }: { c: Content; className?: string }) {
  const o = c.ui.otherLocale;
  return (
    <a href={o.href} hrefLang={o.hrefLang} lang={o.hrefLang} aria-label={o.aria} className={className}>
      <Icon name="globe" className="size-4" />
      {o.label}
    </a>
  );
}

const delay = (d: number) => ({ "--d": d }) as React.CSSProperties;

export function Home({ locale }: { locale: Locale }) {
  const c = getContent(locale);
  const u = c.ui;
  const demo = demoHref(u.demoSubject);
  const visuals = [<AllocationVisual key="a" c={c} />, <RightsizingVisual key="r" c={c} />, <CorrelationVisual key="c" c={c} />];
  const wide = (i: number) => i < 2 || i >= c.features.length - 2;

  return (
    <>
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd(c)) }} />
      <div className="scroll-progress" aria-hidden="true" />

      {/* ------------------------------------------------------------ en-tête */}
      <header className="sticky top-0 z-40 border-b border-white/10 bg-ink/90 text-ink-fg backdrop-blur-xl">
        <div className="mx-auto flex h-16 max-w-7xl items-center gap-6 px-4 sm:px-6">
          <a href="#" className="flex items-center gap-2.5 font-semibold" aria-label={u.home}>
            <Logo className="size-8" />
            <span className="text-lg">Kairn</span>
          </a>
          <nav aria-label={u.mainNav} className="hidden lg:block">
            <ul className="flex gap-6 text-sm text-ink-muted">
              {u.nav.map((n) => (
                <li key={n.href}>
                  <a className="transition hover:text-ink-fg" href={n.href}>
                    {n.label}
                  </a>
                </li>
              ))}
            </ul>
          </nav>
          <div className="ml-auto flex items-center gap-3">
            <LocaleSwitch c={c} className="hidden items-center gap-1.5 rounded-full px-2.5 py-1.5 text-sm text-ink-muted ring-1 ring-white/15 transition hover:text-ink-fg sm:inline-flex" />
            <a href={`${site.appUrl}/login`} className="hidden text-sm text-ink-muted hover:text-ink-fg sm:block">
              {u.login}
            </a>
            <a href={demo} className="rounded-full bg-white px-4 py-2 text-sm font-semibold whitespace-nowrap text-ink transition hover:bg-slate-200">
              {u.demo}
            </a>
            <details className="relative lg:hidden">
              <summary className="cursor-pointer rounded-md p-2 text-ink-muted hover:text-ink-fg" aria-label={u.menu}>
                <Icon name="menu" />
              </summary>
              <ul className="absolute right-0 mt-2 w-56 rounded-xl border border-white/10 bg-ink-2 p-2 text-sm shadow-xl">
                {[...u.nav, { href: docsHref, label: u.docs }, { href: `${site.appUrl}/login`, label: u.login }].map((n) => (
                  <li key={n.href}>
                    <a className="block rounded-md px-3 py-2 text-ink-muted hover:bg-white/5 hover:text-ink-fg" href={n.href}>
                      {n.label}
                    </a>
                  </li>
                ))}
                <li className="mt-1 border-t border-white/10 pt-1">
                  <LocaleSwitch c={c} className="flex items-center gap-2 rounded-md px-3 py-2 text-ink-muted hover:bg-white/5 hover:text-ink-fg" />
                </li>
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
                <Icon name="spark" className="size-3.5" /> {u.hero.badge}
              </p>
              <h1 id="hero-title" className="hero-in mt-7 text-5xl font-semibold tracking-tight text-balance sm:text-7xl" style={delay(1)}>
                {u.hero.titleA}
                <span className="hero-title-glow">{u.hero.titleB}</span>
              </h1>
              <p className="hero-in mx-auto mt-7 max-w-2xl text-lg text-pretty text-ink-muted sm:text-xl" style={delay(2)}>
                {u.hero.text}
              </p>
              <div className="hero-in mt-10 flex flex-wrap items-center justify-center gap-4" style={delay(3)}>
                <PrimaryButton href={demo}>{u.demo}</PrimaryButton>
                <a href="#cascade" className="rounded-full px-6 py-3 text-sm font-semibold text-ink-fg ring-1 ring-white/20 transition hover:bg-white/5">
                  {u.hero.secondary}
                </a>
              </div>
              <ul className="hero-in mt-8 flex flex-wrap justify-center gap-x-6 gap-y-2 text-sm text-ink-muted" style={delay(4)}>
                {u.hero.checks.map((t) => (
                  <li key={t} className="flex items-center gap-1.5">
                    <Icon name="check" className="size-4 text-teal-300" />
                    {t}
                  </li>
                ))}
              </ul>
            </div>
            <div className="hero-in mt-16 sm:mt-20" style={delay(4)}>
              <ProductMock m={c.mock} />
            </div>
          </div>
          <a href="#probleme" className="relative mx-auto mt-14 flex w-fit flex-col items-center gap-2 text-xs text-ink-muted transition hover:text-ink-fg">
            {u.hero.scroll}
            <Icon name="chevron" className="float size-5" />
          </a>
        </section>

        {/* ------------------------------------------------------------ sources */}
        <section aria-labelledby="sources-title" className="border-b border-border bg-bg-2 py-10">
          <h2 id="sources-title" className="px-4 text-center text-sm font-medium text-muted">
            {u.sourcesTitle}
          </h2>
          <div className="marquee mt-6">
            <ul className="marquee-track gap-x-12 gap-y-4 px-6">
              {c.sources.map((s) => (
                <li key={s} className="text-lg font-semibold tracking-tight whitespace-nowrap text-subtle">
                  {s}
                </li>
              ))}
              <li className="marquee-copy" aria-hidden="true">
                {c.sources.map((s) => (
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
              eyebrow={u.problem.eyebrow}
              title={
                <>
                  {u.problem.titleA}
                  <br />
                  <span className="text-brand">{u.problem.titleB}</span>
                </>
              }
              text={u.problem.text}
            />
            <div className="mt-16">
              <InvoiceCompare c={c} />
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
              eyebrow={u.cascade.eyebrow}
              title={
                <>
                  {u.cascade.titleA}
                  <br />
                  <span className="text-gradient">{u.cascade.titleB}</span>
                </>
              }
              text={u.cascade.text}
            />
            <div className="mt-20">
              <Drilldown c={c} />
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ trois questions */}
        <section id="produit" className="py-28" aria-labelledby="produit-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <SectionTitle id="produit-title" eyebrow={u.product.eyebrow} title={u.product.title} text={u.product.text} />
            <div className="mt-20 space-y-28">
              {c.questions.map((q, i) => (
                <article key={q.kicker} className="grid grid-cols-1 items-center gap-12 lg:grid-cols-2 lg:gap-20">
                  <div className={`reveal ${i % 2 ? "lg:order-2" : ""}`}>
                    <p className="flex items-center gap-3 text-sm font-semibold text-brand">
                      <span className="inline-flex size-9 items-center justify-center rounded-full bg-brand-soft font-mono">0{i + 1}</span>
                      {q.kicker}
                    </p>
                    <h3 className="mt-5 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">{q.title}</h3>
                    <p className="mt-5 text-lg leading-relaxed text-pretty text-muted">{q.text}</p>
                    <ul className="mt-7 space-y-3">
                      {u.product.bullets[i].map((b) => (
                        <li key={b} className="flex gap-3">
                          <Icon name="check" className="mt-0.5 size-5 shrink-0 text-brand" />
                          <span>{b}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                  <div className={`reveal-scale ${i % 2 ? "lg:order-1" : ""}`}>{visuals[i]}</div>
                </article>
              ))}
            </div>
          </div>
        </section>

        {/* ------------------------------------------------------------ fonctionnalités */}
        <section className="border-y border-border bg-bg-2 py-28" aria-labelledby="fonctionnalites-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <SectionTitle id="fonctionnalites-title" eyebrow={u.features.eyebrow} title={u.features.title} />
            <div className="mt-16 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
              {c.features.map((f, i) => (
                <article
                  key={f.title}
                  className={`reveal group rounded-2xl border border-border bg-card p-6 transition hover:-translate-y-1 hover:border-brand/40 hover:shadow-xl hover:shadow-teal-900/10 ${wide(i) ? "lg:col-span-2" : ""}`}
                >
                  <span className="inline-flex size-11 items-center justify-center rounded-xl bg-brand-soft text-brand transition group-hover:scale-110">
                    <Icon name={f.icon} />
                  </span>
                  <h3 className={`mt-5 font-semibold tracking-tight ${wide(i) ? "text-xl" : ""}`}>{f.title}</h3>
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
              <p className="text-sm font-semibold tracking-widest text-brand uppercase">{u.ai.eyebrow}</p>
              <h2 id="ia-title" className="mt-4 text-4xl font-semibold tracking-tight text-balance sm:text-5xl">
                {u.ai.title}
              </h2>
              <p className="mt-5 text-lg text-pretty text-muted">{u.ai.text}</p>
              <ul className="mt-8 space-y-3">
                {u.ai.bullets.map((t) => (
                  <li key={t} className="flex gap-3 text-muted">
                    <Icon name="check" className="mt-0.5 size-5 shrink-0 text-brand" />
                    <span>{t}</span>
                  </li>
                ))}
              </ul>
            </div>
            <figure className="reveal-scale relative rounded-3xl border border-border bg-card p-6 shadow-2xl shadow-slate-900/10" aria-label={u.ai.aria}>
              <div className="absolute -inset-4 -z-10 rounded-[2rem] bg-gradient-to-br from-teal-400/20 via-transparent to-indigo-500/20 blur-2xl" aria-hidden="true" />
              <div className="reveal flex justify-end">
                <p className="max-w-[85%] rounded-2xl rounded-br-md bg-brand px-4 py-2.5 text-sm text-pretty text-brand-fg">{u.ai.question}</p>
              </div>
              <div className="reveal reveal-late mt-5 flex gap-3">
                <span className="inline-flex size-8 shrink-0 items-center justify-center rounded-full bg-brand-soft text-brand">
                  <Icon name="spark" className="size-4" />
                </span>
                <div className="space-y-3 text-sm leading-relaxed">
                  {u.ai.answer.map((p, i) => (
                    <p key={i}>
                      <RichText parts={p} />
                    </p>
                  ))}
                  <p className="rounded-lg border border-border bg-bg-2 p-3 text-muted [&_strong]:text-fg">
                    <RichText parts={u.ai.action} />
                  </p>
                  <p className="text-xs text-subtle">{u.ai.sources}</p>
                </div>
              </div>
              <figcaption className="mt-5 border-t border-border pt-3 text-xs text-subtle">{u.ai.caption}</figcaption>
            </figure>
          </div>
        </section>

        {/* ------------------------------------------------------------ personas */}
        <section className="border-y border-border bg-bg-2 py-28" aria-labelledby="equipes-title">
          <div className="mx-auto max-w-7xl px-4 sm:px-6">
            <SectionTitle id="equipes-title" eyebrow={u.personas.eyebrow} title={u.personas.title} />
            <div className="mt-16 grid gap-5 sm:grid-cols-2 lg:grid-cols-4">
              {c.personas.map((p) => (
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
            <SectionTitle id="demarrage-title" eyebrow={u.steps.eyebrow} title={u.steps.title} />
            <ol className="relative mt-16 grid gap-6 md:grid-cols-3">
              {c.steps.map((s) => (
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
              eyebrow={u.sovereignty.eyebrow}
              title={
                <>
                  {u.sovereignty.titleA}
                  <span className="text-gradient">{u.sovereignty.titleB}</span>
                </>
              }
              text={u.sovereignty.text}
            />
            <div className="mt-16 grid gap-5 sm:grid-cols-2 lg:grid-cols-4">
              {c.sovereignty.map((s) => (
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
              <p className="text-sm font-semibold tracking-widest text-brand uppercase">{u.security.eyebrow}</p>
              <h2 id="securite-title" className="mt-4 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
                {u.security.title}
              </h2>
              <ul className="mt-8 space-y-4">
                {c.security.map((s) => (
                  <li key={s} className="flex gap-3">
                    <Icon name="shield" className="mt-0.5 size-5 shrink-0 text-brand" />
                    <span className="text-muted">{s}</span>
                  </li>
                ))}
              </ul>
            </div>
            <div className="reveal">
              <p className="text-sm font-semibold tracking-widest text-brand uppercase">{u.integrations.eyebrow}</p>
              <h2 className="mt-4 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">{u.integrations.title}</h2>
              <div className="mt-8 grid gap-4 sm:grid-cols-2">
                {c.integrations.map((i) => (
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
            <SectionTitle id="tarifs-title" eyebrow={u.pricing.eyebrow} title={u.pricing.title} text={u.pricing.text} />
            <div className="mt-16 grid gap-6 md:grid-cols-2 xl:grid-cols-4">
              {c.plans.map((p) => (
                <article
                  key={p.name}
                  className={`reveal relative flex flex-col rounded-2xl border bg-card p-7 transition hover:-translate-y-1 ${p.highlight ? "border-brand shadow-2xl shadow-teal-900/15 ring-1 ring-brand xl:scale-105" : "border-border hover:shadow-xl hover:shadow-slate-900/5"}`}
                >
                  {p.highlight ? (
                    <p className="absolute -top-3 left-7 rounded-full bg-brand px-3 py-0.5 text-xs font-semibold text-brand-fg">{u.pricing.highlight}</p>
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
                    href={demo}
                    className={`mt-8 rounded-full px-4 py-2.5 text-center text-sm font-semibold transition ${p.highlight ? "bg-brand text-brand-fg hover:bg-brand-strong" : "ring-1 ring-border hover:bg-bg-2"}`}
                  >
                    {p.contact ? u.pricing.contact : u.pricing.demo}
                    <span className="sr-only">
                      {" "}
                      — {u.pricing.planSr} {p.name}
                    </span>
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
              {u.faqTitle}
            </h2>
            <div className="reveal mt-12 divide-y divide-border rounded-2xl border border-border bg-card">
              {c.faq.map((f) => (
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
                {u.cta.titleA}
                <span className="hero-title-glow">{u.cta.titleB}</span>
              </h2>
              <p className="mx-auto mt-6 max-w-xl text-lg text-ink-muted">{u.cta.text}</p>
              <div className="mt-10 flex flex-wrap justify-center gap-4">
                <PrimaryButton href={demo}>{u.demo}</PrimaryButton>
                <a href={docsHref} className="rounded-full px-6 py-3 text-sm font-semibold ring-1 ring-white/20 transition hover:bg-white/5">
                  {u.cta.docs}
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
            <p className="mt-3 text-sm text-muted">{u.footer.tagline}</p>
            <LocaleSwitch c={c} className="mt-5 inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-sm text-muted ring-1 ring-border transition hover:text-fg" />
          </div>
          <nav aria-label={u.footerNav} className="grid grid-cols-2 gap-8 text-sm sm:grid-cols-3">
            <div>
              <p className="font-semibold">{u.footer.product}</p>
              <ul className="mt-3 space-y-2 text-muted">
                <li><a className="hover:text-fg" href="#cascade">{u.footer.howItWorks}</a></li>
                <li><a className="hover:text-fg" href="#produit">{u.footer.features}</a></li>
                <li><a className="hover:text-fg" href="#ia">{u.footer.assistant}</a></li>
                <li><a className="hover:text-fg" href="#tarifs">{u.footer.pricing}</a></li>
              </ul>
            </div>
            <div>
              <p className="font-semibold">{u.footer.resources}</p>
              <ul className="mt-3 space-y-2 text-muted">
                <li><a className="hover:text-fg" href={docsHref}>{u.docs}</a></li>
                <li><a className="hover:text-fg" href={`${docsHref}/connectors`}>{u.footer.connectors}</a></li>
                <li><a className="hover:text-fg" href={`${docsHref}/guide/securite`}>{u.footer.securityLink}</a></li>
              </ul>
            </div>
            <div>
              <p className="font-semibold">{u.footer.contact}</p>
              <ul className="mt-3 space-y-2 text-muted">
                <li><a className="hover:text-fg" href={demo}>{u.demo}</a></li>
                <li><a className="break-all hover:text-fg" href={`mailto:${site.contactEmail}`}>{site.contactEmail}</a></li>
              </ul>
            </div>
          </nav>
        </div>
        <p className="border-t border-border py-6 text-center text-xs text-subtle">
          © {new Date().getFullYear()} Kairn. {u.footer.rights}
        </p>
      </footer>
    </>
  );
}
