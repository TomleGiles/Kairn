import { Card, CardBody, PageHeader } from "@/components/ui/primitives";
import { api, maybe } from "@/lib/api/server";
import { formatNumber } from "@/lib/format";
import { getI18n } from "@/lib/i18n";

import { Chat } from "./chat";

export default async function AssistantPage({ params }: { params: Promise<{ org: string }> }) {
  const { org } = await params;
  const { locale, t } = await getI18n();
  const client = await api();
  const usage = await client.GET("/api/v1/orgs/{org_id}/llm-usage", { params: { path: { org_id: org } } }).then(maybe);
  const used = (usage?.totals?.input_tokens ?? 0) + (usage?.totals?.output_tokens ?? 0);
  return (
    <>
      <PageHeader
        title={t.assistant.title}
        description={t.assistant.subtitle}
        actions={
          usage ? (
            <span className="text-xs text-muted">
              {t.assistant.usage} : {formatNumber(used, locale, 0)} / {formatNumber(usage.quota_tokens, locale, 0)} tokens · {usage.provider}
            </span>
          ) : null
        }
      />
      <Card>
        <CardBody className="p-0">
          <Chat
            org={org}
            locale={locale}
            labels={{ placeholder: t.assistant.placeholder, send: t.assistant.send, sources: t.assistant.sources, thinking: t.assistant.thinking, suggestions: [...t.assistant.suggestions] }}
          />
        </CardBody>
      </Card>
    </>
  );
}
