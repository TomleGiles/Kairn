/** Barres de disponibilité journalière (style page de statut), accessibles. */
export function UptimeBars({ days }: { days: { day: string; uptime: number }[] }) {
  return (
    <ul className="flex h-7 items-stretch gap-[2px]" aria-label="Disponibilité journalière">
      {days.map((d) => {
        const color = d.uptime >= 99.9 ? "bg-success" : d.uptime >= 99 ? "bg-warning" : "bg-danger";
        const label = `${d.day.slice(0, 10)} : ${d.uptime.toFixed(2)} %`;
        return (
          <li key={d.day} className={`w-full min-w-[3px] rounded-[2px] ${color}`} title={label}>
            <span className="sr-only">{label}</span>
          </li>
        );
      })}
    </ul>
  );
}
