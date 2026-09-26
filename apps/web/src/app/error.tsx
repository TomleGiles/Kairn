"use client";

export default function ErrorPage({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <main id="main" className="flex min-h-[60vh] flex-col items-center justify-center gap-3 p-6 text-center">
      <h1 className="text-lg font-semibold">Une erreur est survenue</h1>
      <p className="max-w-md text-sm text-muted">{error.digest ? `Référence : ${error.digest}` : error.message}</p>
      <button className="rounded-md border border-border bg-surface px-4 py-2 text-sm hover:bg-surface-2" onClick={reset}>
        Réessayer
      </button>
    </main>
  );
}
