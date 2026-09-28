import type { Rich } from "@/lib/content";

/** Affiche un texte enrichi (segments simples, en gras ou en code). */
export function RichText({ parts, codeClassName = "rounded bg-bg-2 px-1 font-mono text-xs" }: { parts: Rich; codeClassName?: string }) {
  return (
    <>
      {parts.map((p, i) =>
        typeof p === "string" ? (
          p
        ) : "b" in p ? (
          <strong key={i}>{p.b}</strong>
        ) : (
          <code key={i} className={codeClassName}>
            {p.code}
          </code>
        ),
      )}
    </>
  );
}
