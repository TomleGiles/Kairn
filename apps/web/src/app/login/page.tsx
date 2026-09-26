import type { Metadata } from "next";

import { Logo } from "@/components/logo";
import { getI18n } from "@/lib/i18n";

import { LoginForm } from "./login-form";

export const metadata: Metadata = { title: "Connexion" };

export default async function LoginPage({ searchParams }: { searchParams: Promise<{ next?: string }> }) {
  const { t } = await getI18n();
  const { next } = await searchParams;
  const oidc = process.env.KAIRN_OIDC_ENABLED === "true";
  const devLogin = process.env.KAIRN_DEV_LOGIN !== "false";
  return (
    <main id="main" className="flex min-h-screen items-center justify-center p-6">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex items-center gap-3">
          <Logo />
          <div>
            <p className="text-lg font-semibold">Kairn</p>
            <p className="text-xs text-muted">{t.app.tagline}</p>
          </div>
        </div>
        <div className="rounded-xl border border-border bg-surface p-6 shadow-sm">
          <h1 className="text-lg font-semibold">{t.login.title}</h1>
          <p className="mt-1 text-sm text-muted">{t.login.subtitle}</p>
          <div className="mt-6 space-y-4">
            {oidc ? (
              <a
                href={`/api/v1/auth/oidc/login?redirect=${encodeURIComponent(next ?? "/")}`}
                className="flex h-10 w-full items-center justify-center rounded-md border border-border bg-surface text-sm font-medium hover:bg-surface-2"
              >
                {t.login.sso}
              </a>
            ) : null}
            {oidc && devLogin ? <p className="text-center text-xs text-subtle">{t.login.or}</p> : null}
            {devLogin ? <LoginForm labels={{ email: t.login.email, submit: t.login.submit }} next={next ?? "/"} /> : null}
          </div>
          {devLogin ? <p className="mt-6 text-xs text-subtle">{t.login.demoHint}</p> : null}
        </div>
      </div>
    </main>
  );
}
