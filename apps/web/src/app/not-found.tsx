import Link from "next/link";

export default function NotFound() {
  return (
    <main id="main" className="flex min-h-[60vh] flex-col items-center justify-center gap-3 p-6 text-center">
      <p className="text-5xl font-semibold text-subtle">404</p>
      <h1 className="text-lg font-semibold">Page introuvable</h1>
      <p className="max-w-md text-sm text-muted">Cette page n&apos;existe pas ou vous n&apos;y avez pas accès.</p>
      <Link className="text-sm text-brand hover:underline" href="/">
        Retour à l&apos;accueil
      </Link>
    </main>
  );
}
