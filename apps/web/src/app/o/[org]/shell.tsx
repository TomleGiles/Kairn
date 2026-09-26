"use client";

import {
  Activity,
  AlertTriangle,
  BarChart3,
  Bot,
  Boxes,
  Building2,
  FileText,
  Gauge,
  GitFork,
  Layers,
  LayoutDashboard,
  Lightbulb,
  LogOut,
  Menu,
  Moon,
  Network,
  PiggyBank,
  Plug,
  Settings,
  Sun,
  TrendingUp,
  X,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useState } from "react";

import { Logo } from "@/components/logo";
import { browserApi } from "@/lib/api/browser";
import type { Locale } from "@/lib/format";
import type { Dictionary } from "@/lib/i18n/fr";
import { cn, initials } from "@/lib/utils";

type NavItem = { href: string; label: string; icon: React.ComponentType<{ className?: string }>; perm?: string };

export function Shell({
  children,
  org,
  orgs,
  user,
  locale,
  t,
}: {
  children: React.ReactNode;
  org: { id: string; name: string; plan: string; parent: string | null };
  orgs: { id: string; name: string; plan: string }[];
  user: { name: string; email: string };
  locale: Locale;
  t: Pick<Dictionary, "nav" | "common">;
}) {
  const pathname = usePathname();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const base = `/o/${org.id}`;
  const sections: { title: string; items: NavItem[] }[] = [
    {
      title: t.nav.sectionAnalyze,
      items: [
        { href: base, label: t.nav.overview, icon: LayoutDashboard },
        { href: `${base}/costs`, label: t.nav.costs, icon: BarChart3 },
        { href: `${base}/resources`, label: t.nav.inventory, icon: Boxes },
        { href: `${base}/topology`, label: t.nav.topology, icon: Network },
        { href: `${base}/efficiency`, label: t.nav.efficiency, icon: Gauge },
        { href: `${base}/allocation`, label: t.nav.allocation, icon: GitFork },
      ],
    },
    {
      title: t.nav.sectionAct,
      items: [
        { href: `${base}/recommendations`, label: t.nav.recommendations, icon: Lightbulb },
        { href: `${base}/anomalies`, label: t.nav.anomalies, icon: AlertTriangle },
        { href: `${base}/budgets`, label: t.nav.budgets, icon: PiggyBank },
        { href: `${base}/forecast`, label: t.nav.forecast, icon: TrendingUp },
        { href: `${base}/assistant`, label: t.nav.assistant, icon: Bot },
        { href: `${base}/reports`, label: t.nav.reports, icon: FileText },
      ],
    },
    {
      title: t.nav.sectionOperate,
      items: [
        { href: `${base}/uptime`, label: t.nav.uptime, icon: Activity },
        { href: `${base}/connectors`, label: t.nav.connectors, icon: Plug },
        ...(org.plan === "msp" ? [{ href: `${base}/clients`, label: t.nav.clients, icon: Building2 }] : []),
        { href: `${base}/settings`, label: t.nav.settings, icon: Settings },
      ],
    },
  ];
  const isActive = (href: string) => (href === base ? pathname === base : pathname.startsWith(href));
  const setCookie = (name: string, value: string) => {
    document.cookie = `${name}=${value}; path=/; max-age=31536000; samesite=lax`;
  };
  const toggleTheme = () => {
    const dark = !document.documentElement.classList.contains("dark");
    document.documentElement.classList.toggle("dark", dark);
    setCookie("kairn_theme", dark ? "dark" : "light");
  };
  const nav = (
    <nav aria-label="Navigation principale" className="flex flex-1 flex-col gap-6 overflow-y-auto px-3 py-4">
      {sections.map((s) => (
        <div key={s.title}>
          <p className="px-2 pb-1 text-[11px] font-semibold uppercase tracking-wider text-subtle">{s.title}</p>
          <ul className="space-y-0.5">
            {s.items.map((it) => {
              const active = isActive(it.href);
              const Icon = it.icon;
              return (
                <li key={it.href}>
                  <Link
                    href={it.href}
                    onClick={() => setOpen(false)}
                    aria-current={active ? "page" : undefined}
                    className={cn(
                      "flex items-center gap-2.5 rounded-md px-2 py-1.5 text-sm",
                      active ? "bg-brand-soft font-medium text-brand" : "text-muted hover:bg-surface-2 hover:text-foreground",
                    )}
                  >
                    <Icon className="size-4 shrink-0" />
                    {it.label}
                  </Link>
                </li>
              );
            })}
          </ul>
        </div>
      ))}
    </nav>
  );
  return (
    <div className="flex min-h-screen">
      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-40 flex w-64 flex-col border-r border-border bg-surface transition-transform lg:static lg:translate-x-0",
          open ? "translate-x-0" : "-translate-x-full",
        )}
      >
        <div className="flex h-14 items-center gap-2 border-b border-border px-4">
          <Logo size={28} />
          <span className="font-semibold">Kairn</span>
          <button className="ml-auto lg:hidden" onClick={() => setOpen(false)} aria-label={t.common.close}>
            <X className="size-5" />
          </button>
        </div>
        <div className="border-b border-border px-3 py-3">
          <label htmlFor="org-switch" className="sr-only">
            Organisation
          </label>
          <div className="flex items-center gap-2">
            <Layers className="size-4 text-subtle" aria-hidden />
            <select
              id="org-switch"
              className="h-8 w-full truncate rounded-md border border-border bg-surface px-2 text-sm"
              value={org.id}
              onChange={(e) => router.push(`/o/${e.target.value}`)}
            >
              {orgs.map((o) => (
                <option key={o.id} value={o.id}>
                  {o.name}
                </option>
              ))}
            </select>
          </div>
          <p className="mt-1 pl-6 text-[11px] uppercase tracking-wide text-subtle">Plan {org.plan}</p>
        </div>
        {nav}
        <div className="border-t border-border p-3">
          <div className="flex items-center gap-2">
            <span className="flex size-8 items-center justify-center rounded-full bg-brand-soft text-xs font-semibold text-brand" aria-hidden>
              {initials(user.name || user.email)}
            </span>
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">{user.name}</p>
              <p className="truncate text-xs text-subtle">{user.email}</p>
            </div>
          </div>
          <div className="mt-3 flex items-center gap-1">
            <button className="rounded-md p-1.5 text-muted hover:bg-surface-2" onClick={toggleTheme} aria-label={`${t.common.theme} : ${t.common.light} / ${t.common.dark}`}>
              <Sun className="size-4 dark:hidden" />
              <Moon className="hidden size-4 dark:block" />
            </button>
            <label htmlFor="locale-switch" className="sr-only">
              {t.common.language}
            </label>
            <select
              id="locale-switch"
              className="h-7 rounded-md border border-border bg-surface px-1 text-xs"
              value={locale}
              onChange={(e) => {
                setCookie("kairn_locale", e.target.value);
                router.refresh();
              }}
            >
              <option value="fr">FR</option>
              <option value="en">EN</option>
            </select>
            <button
              className="ml-auto flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted hover:bg-surface-2"
              onClick={async () => {
                await browserApi.POST("/api/v1/auth/logout");
                router.replace("/login");
                router.refresh();
              }}
            >
              <LogOut className="size-3.5" /> {t.common.signOut}
            </button>
          </div>
        </div>
      </aside>
      {open ? <div className="fixed inset-0 z-30 bg-black/30 lg:hidden" onClick={() => setOpen(false)} aria-hidden /> : null}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center gap-3 border-b border-border bg-surface px-4 lg:hidden">
          <button onClick={() => setOpen(true)} aria-label="Menu" aria-expanded={open}>
            <Menu className="size-5" />
          </button>
          <span className="font-semibold">{org.name}</span>
        </header>
        <main id="main" className="mx-auto w-full max-w-[1400px] flex-1 px-4 py-6 lg:px-8">
          {children}
        </main>
      </div>
    </div>
  );
}
