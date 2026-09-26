"use client";

import { Bot, Send, User, Wrench } from "lucide-react";
import { Fragment, useRef, useState } from "react";

import { Donut, StackedBars } from "@/components/charts/charts";
import { Button, Textarea } from "@/components/ui/primitives";
import type { Locale } from "@/lib/format";

type ChartSpec = { kind: "bar" | "line" | "donut"; title: string; categories: string[]; series: { name: string; values: number[] }[]; currency?: string };
type Msg = { role: "user" | "assistant"; content: string; tools?: string[]; charts?: ChartSpec[]; sources?: string[]; error?: string; warning?: string };

/** Rendu minimal et sûr du texte de l'assistant (paragraphes, listes, gras). */
function RichText({ text }: { text: string }) {
  const blocks = text.split(/\n{2,}/);
  const inline = (s: string) =>
    s.split(/(\*\*[^*]+\*\*)/g).map((part, i) => (part.startsWith("**") && part.endsWith("**") ? <strong key={i}>{part.slice(2, -2)}</strong> : <Fragment key={i}>{part}</Fragment>));
  return (
    <div className="space-y-2 text-sm leading-relaxed">
      {blocks.map((b, i) => {
        const lines = b.split("\n");
        if (lines.every((l) => /^\s*([-*]|\d+\.)\s/.test(l))) {
          return (
            <ul key={i} className="list-disc space-y-0.5 pl-5">
              {lines.map((l, j) => (
                <li key={j}>{inline(l.replace(/^\s*([-*]|\d+\.)\s/, ""))}</li>
              ))}
            </ul>
          );
        }
        return (
          <p key={i} className="whitespace-pre-wrap">
            {inline(b)}
          </p>
        );
      })}
    </div>
  );
}

export function Chat({ org, locale, labels }: { org: string; locale: Locale; labels: { placeholder: string; send: string; sources: string; thinking: string; suggestions: string[] } }) {
  const [messages, setMessages] = useState<Msg[]>([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const endRef = useRef<HTMLDivElement>(null);

  async function send(text: string) {
    if (!text.trim() || busy) return;
    const history: Msg[] = [...messages, { role: "user", content: text }];
    setMessages([...history, { role: "assistant", content: "", tools: [], charts: [], sources: [] }]);
    setInput("");
    setBusy(true);
    const update = (fn: (m: Msg) => Msg) => setMessages((ms) => [...ms.slice(0, -1), fn(ms[ms.length - 1])]);
    try {
      const res = await fetch(`/api/v1/orgs/${org}/assistant/chat`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
        body: JSON.stringify({ locale, messages: history.map((m) => ({ role: m.role, content: m.content || "…" })) }),
      });
      if (!res.ok || !res.body) {
        const err = await res.json().catch(() => ({}));
        update((m) => ({ ...m, error: (err as { detail?: string }).detail ?? `${res.status}` }));
        return;
      }
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = "";
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        const parts = buf.split("\n\n");
        buf = parts.pop() ?? "";
        for (const part of parts) {
          const line = part.split("\n").find((l) => l.startsWith("data:"));
          if (!line) continue;
          let ev: { type: string; text?: string; name?: string; spec?: ChartSpec; sources?: string[]; message?: string };
          try {
            ev = JSON.parse(line.slice(5).trim());
          } catch {
            continue;
          }
          if (ev.type === "text") update((m) => ({ ...m, content: m.content + (ev.text ?? "") }));
          else if (ev.type === "reset") update((m) => ({ ...m, content: "" }));
          else if (ev.type === "warning") update((m) => ({ ...m, warning: ev.message }));
          else if (ev.type === "tool_call") update((m) => ({ ...m, tools: [...(m.tools ?? []), ev.name ?? ""] }));
          else if (ev.type === "chart" && ev.spec) update((m) => ({ ...m, charts: [...(m.charts ?? []), ev.spec!] }));
          else if (ev.type === "sources") update((m) => ({ ...m, sources: ev.sources ?? [] }));
          else if (ev.type === "error") update((m) => ({ ...m, error: ev.message }));
        }
        endRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
      }
    } catch (e) {
      update((m) => ({ ...m, error: String(e) }));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex h-[70vh] flex-col">
      <div className="flex-1 space-y-4 overflow-y-auto p-5" aria-live="polite" aria-busy={busy}>
        {messages.length === 0 ? (
          <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
            {labels.suggestions.map((s) => (
              <button key={s} className="rounded-lg border border-border p-3 text-left text-sm text-muted hover:bg-surface-2 hover:text-foreground" onClick={() => send(s)}>
                {s}
              </button>
            ))}
          </div>
        ) : null}
        {messages.map((m, i) => (
          <div key={i} className="flex gap-3">
            <span className={`mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full ${m.role === "user" ? "bg-surface-2" : "bg-brand-soft text-brand"}`} aria-hidden>
              {m.role === "user" ? <User className="size-4" /> : <Bot className="size-4" />}
            </span>
            <div className="min-w-0 flex-1 space-y-2">
              {m.tools?.length ? (
                <p className="flex flex-wrap items-center gap-1 text-xs text-subtle">
                  <Wrench className="size-3" aria-hidden /> {m.tools.join(" · ")}
                </p>
              ) : null}
              {m.content ? <RichText text={m.content} /> : m.role === "assistant" && busy && i === messages.length - 1 ? <p className="text-sm text-muted">{labels.thinking}</p> : null}
              {m.charts?.map((c, j) =>
                c.kind === "donut" ? (
                  <Donut key={j} label={c.title} items={c.categories.map((cat, k) => ({ name: cat, value: c.series[0]?.values[k] ?? 0 }))} currency={c.currency ?? "EUR"} locale={locale} />
                ) : (
                  <StackedBars key={j} label={c.title} categories={c.categories} series={c.series} currency={c.currency ?? "EUR"} locale={locale} height={240} />
                ),
              )}
              {m.sources?.length ? (
                <details className="text-xs text-muted">
                  <summary className="cursor-pointer">{labels.sources} ({m.sources.length})</summary>
                  <ul className="mt-1 list-disc pl-4 font-mono">
                    {m.sources.map((s, k) => (
                      <li key={k}>{s}</li>
                    ))}
                  </ul>
                </details>
              ) : null}
              {m.warning ? (
                <p role="status" className="text-xs text-warning">
                  {m.warning}
                </p>
              ) : null}
              {m.error ? (
                <p role="alert" className="text-sm text-danger">
                  {m.error}
                </p>
              ) : null}
            </div>
          </div>
        ))}
        <div ref={endRef} />
      </div>
      <form
        className="flex items-end gap-2 border-t border-border p-3"
        onSubmit={(e) => {
          e.preventDefault();
          void send(input);
        }}
      >
        <label htmlFor="chat-input" className="sr-only">
          {labels.placeholder}
        </label>
        <Textarea
          id="chat-input"
          rows={2}
          value={input}
          placeholder={labels.placeholder}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              void send(input);
            }
          }}
        />
        <Button type="submit" disabled={busy || !input.trim()} aria-label={labels.send}>
          <Send />
        </Button>
      </form>
    </div>
  );
}
