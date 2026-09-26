import { PageHeader } from "@/components/ui/primitives";
import { getI18n } from "@/lib/i18n";

import { SettingsTabs } from "./tabs";

export default async function SettingsLayout({ children, params }: { children: React.ReactNode; params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { t } = await getI18n();
  const base = `/o/${org}/settings`;
  return (
    <>
      <PageHeader title={t.settings.title} />
      <SettingsTabs
        tabs={[
          { href: base, label: t.settings.organization },
          { href: `${base}/members`, label: t.settings.members },
          { href: `${base}/tokens`, label: t.settings.tokens },
          { href: `${base}/pricing`, label: t.settings.pricing },
          { href: `${base}/billing`, label: t.settings.billing },
          { href: `${base}/audit`, label: t.settings.audit },
        ]}
      />
      <div className="mt-4">{children}</div>
    </>
  );
}
