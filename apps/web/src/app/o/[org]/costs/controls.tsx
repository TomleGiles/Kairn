"use client";

import { X } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";

import { Badge, Select } from "@/components/ui/primitives";

export function CostControls({
  dims,
  group,
  gran,
  preset,
  filters,
  labels,
}: {
  dims: { value: string; label: string }[];
  group: string;
  gran: string;
  preset: string;
  filters: string[];
  labels: Record<"groupBy" | "granularity" | "period" | "day" | "week" | "month" | "last30" | "last90" | "thisMonth" | "lastMonth" | "filter", string>;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const go = (next: Partial<{ group: string; gran: string; range: string; filters: string[] }>) => {
    const q = new URLSearchParams({ group: next.group ?? group, gran: next.gran ?? gran, range: next.range ?? preset });
    (next.filters ?? filters).forEach((f) => q.append("filter", f));
    router.push(`${pathname}?${q}`);
  };
  return (
    <div className="flex flex-wrap items-end gap-3">
      <div>
        <label htmlFor="cc-group" className="mb-1 block text-xs font-medium text-muted">
          {labels.groupBy}
        </label>
        <Select id="cc-group" value={group} onChange={(e) => go({ group: e.target.value })}>
          {dims.map((d) => (
            <option key={d.value} value={d.value}>
              {d.label}
            </option>
          ))}
        </Select>
      </div>
      <div>
        <label htmlFor="cc-gran" className="mb-1 block text-xs font-medium text-muted">
          {labels.granularity}
        </label>
        <Select id="cc-gran" value={gran} onChange={(e) => go({ gran: e.target.value })}>
          <option value="day">{labels.day}</option>
          <option value="week">{labels.week}</option>
          <option value="month">{labels.month}</option>
        </Select>
      </div>
      <div>
        <label htmlFor="cc-range" className="mb-1 block text-xs font-medium text-muted">
          {labels.period}
        </label>
        <Select id="cc-range" value={preset} onChange={(e) => go({ range: e.target.value })}>
          <option value="30d">{labels.last30}</option>
          <option value="90d">{labels.last90}</option>
          <option value="month">{labels.thisMonth}</option>
          <option value="lastmonth">{labels.lastMonth}</option>
        </Select>
      </div>
      {filters.length ? (
        <div className="flex flex-wrap items-center gap-1.5" aria-label={labels.filter}>
          {filters.map((f) => (
            <Badge key={f} tone="brand">
              {f.length > 40 ? f.slice(0, 40) + "…" : f}
              <button aria-label={`Retirer ${f}`} onClick={() => go({ filters: filters.filter((x) => x !== f) })}>
                <X className="size-3" />
              </button>
            </Badge>
          ))}
        </div>
      ) : null}
    </div>
  );
}
