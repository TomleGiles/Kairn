export function Logo({ size = 36 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 36 36" aria-hidden>
      <rect width="36" height="36" rx="9" fill="var(--brand)" />
      <path d="M9 26h18M12 26l3-7h6l3 7M15 19l1.5-5h3L21 19M17 14l1-4 1 4" stroke="var(--brand-fg)" strokeWidth="2" fill="none" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
