"use client";

import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";

import { Button, type ButtonProps } from "@/components/ui/primitives";
import { problemMessage } from "@/lib/api/browser";

export type Method = "POST" | "PUT" | "PATCH" | "DELETE";

/** Appelle l'API (même origine) et renvoie les données ou une erreur lisible. */
export async function callApi<T = unknown>(method: Method | "GET", path: string, body?: unknown): Promise<{ data?: T; error?: string }> {
  const res = await fetch(path, {
    method,
    credentials: "include",
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204 || res.status === 202) return {};
  const text = await res.text();
  let json: unknown = undefined;
  try {
    json = text ? JSON.parse(text) : undefined;
  } catch {
    json = undefined;
  }
  if (!res.ok) return { error: problemMessage(json, `${res.status} ${res.statusText}`) };
  return { data: json as T };
}

/** Hook d'appel API avec état de chargement, erreur et rafraîchissement de la page. */
export function useApi() {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  async function run<T = unknown>(method: Method, path: string, body?: unknown, refresh = true): Promise<T | undefined> {
    setBusy(true);
    setError(null);
    const res = await callApi<T>(method, path, body);
    setBusy(false);
    if (res.error) {
      setError(res.error);
      return undefined;
    }
    if (refresh) startTransition(() => router.refresh());
    return res.data ?? ({} as T);
  }
  return { run, error, setError, busy: busy || pending };
}

export function ErrorText({ error }: { error: string | null }) {
  if (!error) return null;
  return (
    <p role="alert" className="text-sm text-danger">
      {error}
    </p>
  );
}

/** Bouton déclenchant un appel API (confirmation optionnelle). */
export function ApiButton({
  method,
  path,
  body,
  confirm,
  children,
  onDone,
  followUrl,
  ...props
}: Omit<ButtonProps, "onClick"> & { method: Method; path: string; body?: unknown; confirm?: string; onDone?: () => void; followUrl?: boolean }) {
  const { run, error, busy } = useApi();
  return (
    <span className="inline-flex flex-col gap-1">
      <Button
        {...props}
        disabled={busy || props.disabled}
        onClick={async () => {
          if (confirm && !window.confirm(confirm)) return;
          const res = await run<{ url?: string }>(method, path, body, !followUrl);
          if (res !== undefined) onDone?.();
          // Redirection externe (ex. paiement Stripe) : uniquement vers https.
          if (followUrl && res?.url && res.url.startsWith("https://")) window.location.href = res.url;
        }}
      >
        {children}
      </Button>
      <ErrorText error={error} />
    </span>
  );
}

/** Bouton de copie dans le presse-papiers. */
export function CopyButton({ value, label, copied }: { value: string; label: string; copied: string }) {
  const [done, setDone] = useState(false);
  return (
    <Button
      variant="secondary"
      size="sm"
      onClick={async () => {
        await navigator.clipboard.writeText(value);
        setDone(true);
        setTimeout(() => setDone(false), 1500);
      }}
    >
      <span aria-live="polite">{done ? copied : label}</span>
    </Button>
  );
}
