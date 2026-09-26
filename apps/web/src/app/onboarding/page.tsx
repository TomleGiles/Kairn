import { Logo } from "@/components/logo";
import { getI18n } from "@/lib/i18n";

import { OnboardingForm } from "./form";

export default async function OnboardingPage() {
  const { t } = await getI18n();
  return (
    <main id="main" className="mx-auto max-w-xl px-4 py-16">
      <div className="mb-8 flex items-center gap-3">
        <Logo />
        <div>
          <h1 className="text-xl font-semibold">{t.onboarding.title}</h1>
          <p className="text-sm text-muted">{t.onboarding.subtitle}</p>
        </div>
      </div>
      <ol className="mb-6 flex gap-4 text-xs text-muted" aria-label="Étapes">
        <li className="font-semibold text-brand">1. {t.onboarding.step1}</li>
        <li>2. {t.onboarding.step2}</li>
        <li>3. {t.onboarding.step3}</li>
      </ol>
      <OnboardingForm labels={{ orgName: t.onboarding.orgName, create: t.common.next }} />
    </main>
  );
}
