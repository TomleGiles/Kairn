"use client";

import { useCallback } from "react";

import { formatMoney, formatMoneyCompact, type Locale } from "@/lib/format";

import { EChart } from "./echart";

type Money = { currency: string; locale: Locale };

function money(v: number, m: Money): string {
  return formatMoney(v.toFixed(2), m.currency, m.locale);
}

// Les options ECharts sont mémorisées sur une clé JSON des données : un parent
// qui recrée ses tableaux à chaque rendu ne relance pas le graphique. Le
// callback relit les données depuis la clé (dépendances simples et exactes).
function parse<T>(key: string): T {
  return JSON.parse(key) as T;
}

type BarData = { categories: string[]; series: { name: string; values: number[] }[] };

/** Barres journalières (empilées si plusieurs séries). */
export function StackedBars({
  label,
  categories,
  series,
  currency,
  locale,
  height = 280,
}: { label: string; categories: string[]; series: { name: string; values: number[] }[]; height?: number } & Money) {
  const dataKey = JSON.stringify({ categories, series });
  const option = useCallback(
    (colors: string[], text: { fg: string; muted: string; border: string }) => {
      const d = parse<BarData>(dataKey);
      const m = { currency, locale };
      return {
        color: colors,
        grid: { left: 8, right: 8, top: d.series.length > 1 ? 36 : 12, bottom: 8, containLabel: true },
        legend: d.series.length > 1 ? { top: 0, type: "scroll", textStyle: { color: text.muted } } : undefined,
        tooltip: {
          trigger: "axis",
          axisPointer: { type: "shadow" },
          valueFormatter: (v: number) => money(v, m),
        },
        xAxis: { type: "category", data: d.categories, axisLine: { lineStyle: { color: text.border } }, axisLabel: { color: text.muted } },
        yAxis: {
          type: "value",
          axisLabel: { color: text.muted, formatter: (v: number) => formatMoneyCompact(v, currency, locale) },
          splitLine: { lineStyle: { color: text.border } },
        },
        series: d.series.map((s) => ({ name: s.name, type: "bar", stack: "total", data: s.values, barMaxWidth: 28, emphasis: { focus: "series" } })),
      };
    },
    [dataKey, currency, locale],
  );
  const m = { currency, locale };
  const rows = categories.map((c, i) => [c, ...series.map((s) => money(s.values[i] ?? 0, m))]);
  return <EChart option={option} height={height} label={label} table={{ headers: ["", ...series.map((s) => s.name)], rows }} />;
}

/** Répartition en anneau. */
export function Donut({ label, items, currency, locale, height = 280 }: { label: string; items: { name: string; value: number }[]; height?: number } & Money) {
  const dataKey = JSON.stringify(items);
  const option = useCallback(
    (colors: string[], text: { fg: string; muted: string }) => {
      const m = { currency, locale };
      return {
        color: colors,
        tooltip: { trigger: "item", valueFormatter: (v: number) => money(v, m) },
        legend: { orient: "horizontal", bottom: 0, left: "center", type: "scroll", textStyle: { color: text.muted } },
        series: [
          {
            type: "pie",
            radius: ["48%", "72%"],
            center: ["50%", "42%"],
            avoidLabelOverlap: true,
            label: { show: false },
            data: parse<{ name: string; value: number }[]>(dataKey),
          },
        ],
      };
    },
    [dataKey, currency, locale],
  );
  const m = { currency, locale };
  return <EChart option={option} height={height} label={label} table={{ headers: ["", label], rows: items.map((i) => [i.name, money(i.value, m)]) }} />;
}

type ForecastData = {
  history: { day: string; value: number }[];
  forecast: { day: string; value: number; lower: number; upper: number }[];
  labels: { history: string; forecast: string; interval: string };
};

/** Historique + prévision avec intervalle de confiance. */
export function ForecastChart({
  label,
  history,
  forecast,
  currency,
  locale,
  labels,
}: {
  label: string;
  history: { day: string; value: number }[];
  forecast: { day: string; value: number; lower: number; upper: number }[];
  labels: { history: string; forecast: string; interval: string };
} & Money) {
  const dataKey = JSON.stringify({ history, forecast, labels });
  const option = useCallback(
    (colors: string[], text: { fg: string; muted: string; border: string }) => {
      const { history: h, forecast: f, labels: l } = parse<ForecastData>(dataKey);
      const m = { currency, locale };
      const days = [...h.map((x) => x.day), ...f.map((x) => x.day)];
      const pad = (n: number) => Array<number | null>(n).fill(null);
      return {
        color: colors,
        grid: { left: 8, right: 8, top: 36, bottom: 8, containLabel: true },
        legend: { top: 0, data: [l.history, l.forecast, l.interval], textStyle: { color: text.muted } },
        tooltip: { trigger: "axis", valueFormatter: (v: number | null) => (v == null ? "—" : money(v, m)) },
        xAxis: { type: "category", data: days, axisLabel: { color: text.muted }, axisLine: { lineStyle: { color: text.border } } },
        yAxis: {
          type: "value",
          axisLabel: { color: text.muted, formatter: (v: number) => formatMoneyCompact(v, currency, locale) },
          splitLine: { lineStyle: { color: text.border } },
        },
        series: [
          { name: l.history, type: "bar", data: [...h.map((x) => x.value), ...pad(f.length)], barMaxWidth: 16, itemStyle: { color: colors[0] } },
          { name: "lower", type: "line", data: [...pad(h.length), ...f.map((x) => x.lower)], stack: "band", lineStyle: { opacity: 0 }, symbol: "none", tooltip: { show: false } },
          {
            name: l.interval,
            type: "line",
            data: [...pad(h.length), ...f.map((x) => x.upper - x.lower)],
            stack: "band",
            lineStyle: { opacity: 0 },
            symbol: "none",
            areaStyle: { color: colors[1], opacity: 0.15 },
            itemStyle: { color: colors[1] },
          },
          { name: l.forecast, type: "line", data: [...pad(h.length), ...f.map((x) => x.value)], lineStyle: { type: "dashed", width: 2 }, itemStyle: { color: colors[1] }, symbol: "none" },
        ],
      };
    },
    [dataKey, currency, locale],
  );
  const m = { currency, locale };
  const rows = [
    ...history.map((h) => [h.day, money(h.value, m), ""]),
    ...forecast.map((f) => [f.day, money(f.value, m), `${money(f.lower, m)} – ${money(f.upper, m)}`]),
  ];
  return <EChart option={option} height={300} label={label} table={{ headers: ["", labels.forecast, labels.interval], rows }} />;
}

/** Courbes d'usage (pourcentages ou valeurs brutes). */
export function UsageLines({
  label,
  series,
  unit = "percent",
  height = 240,
}: { label: string; series: { name: string; points: [string, number][] }[]; unit?: "percent" | "cores" | "iops" | "raw"; height?: number }) {
  const dataKey = JSON.stringify(series);
  const option = useCallback(
    (colors: string[], text: { fg: string; muted: string; border: string }) => ({
      color: colors,
      grid: { left: 8, right: 8, top: 32, bottom: 8, containLabel: true },
      legend: { top: 0, type: "scroll", textStyle: { color: text.muted } },
      tooltip: { trigger: "axis", valueFormatter: (v: number) => (unit === "percent" ? `${(v * 100).toFixed(1)} %` : v.toFixed(2)) },
      xAxis: { type: "time", axisLabel: { color: text.muted }, axisLine: { lineStyle: { color: text.border } } },
      yAxis: {
        type: "value",
        max: unit === "percent" ? 1 : undefined,
        axisLabel: { color: text.muted, formatter: (v: number) => (unit === "percent" ? `${Math.round(v * 100)} %` : String(v)) },
        splitLine: { lineStyle: { color: text.border } },
      },
      series: parse<{ name: string; points: [string, number][] }[]>(dataKey).map((s) => ({
        name: s.name,
        type: "line",
        data: s.points,
        showSymbol: false,
        smooth: true,
        lineStyle: { width: 1.5 },
      })),
    }),
    [dataKey, unit],
  );
  return <EChart option={option} height={height} label={label} />;
}

type TopologyData = {
  nodes: { id: string; name: string; type: string; cost: number }[];
  edges: { source: string; target: string; relation: string }[];
};

/** Graphe de topologie (force). */
export function TopologyGraph({
  label,
  nodes,
  edges,
  currency,
  locale,
}: { label: string; nodes: { id: string; name: string; type: string; cost: number }[]; edges: { source: string; target: string; relation: string }[] } & Money) {
  const dataKey = JSON.stringify({ nodes, edges });
  const option = useCallback(
    (colors: string[], text: { fg: string; muted: string; border: string }) => {
      const d = parse<TopologyData>(dataKey);
      const m = { currency, locale };
      const types = Array.from(new Set(d.nodes.map((n) => n.type)));
      const max = Math.max(1, ...d.nodes.map((n) => n.cost));
      return {
        color: colors,
        tooltip: {
          formatter: (p: { dataType: string; data: { name?: string; type?: string; cost?: number; relation?: string } }) =>
            p.dataType === "edge" ? p.data.relation ?? "" : `${p.data.name}<br/>${p.data.type}<br/>${money(p.data.cost ?? 0, m)} (30 j)`,
        },
        legend: { top: 0, type: "scroll", data: types, textStyle: { color: text.muted } },
        series: [
          {
            type: "graph",
            layout: "force",
            roam: true,
            draggable: true,
            categories: types.map((t) => ({ name: t })),
            force: { repulsion: 140, edgeLength: [40, 110], gravity: 0.08 },
            label: { show: true, position: "right", color: text.fg, fontSize: 10, formatter: "{b}" },
            lineStyle: { color: text.border, width: 1, curveness: 0.1 },
            data: d.nodes.map((n) => ({
              id: n.id,
              name: n.name,
              type: n.type,
              cost: n.cost,
              category: types.indexOf(n.type),
              symbolSize: 8 + 26 * Math.sqrt(n.cost / max),
            })),
            links: d.edges.map((e) => ({ source: e.source, target: e.target, relation: e.relation })),
          },
        ],
      };
    },
    [dataKey, currency, locale],
  );
  const m = { currency, locale };
  return (
    <EChart
      option={option}
      height={560}
      label={label}
      table={{ headers: ["", "", ""], rows: nodes.slice(0, 200).map((n) => [n.name, n.type, money(n.cost, m)]) }}
    />
  );
}

/** Mini-courbe sans axes (tuiles). */
export function Sparkline({ values, label }: { values: number[]; label: string }) {
  const dataKey = JSON.stringify(values);
  const option = useCallback(
    (colors: string[]) => {
      const v = parse<number[]>(dataKey);
      return {
        grid: { left: 0, right: 0, top: 2, bottom: 2 },
        xAxis: { type: "category", show: false, data: v.map((_, i) => i) },
        yAxis: { type: "value", show: false, min: "dataMin" },
        series: [{ type: "line", data: v, showSymbol: false, smooth: true, lineStyle: { width: 1.5, color: colors[0] }, areaStyle: { color: colors[0], opacity: 0.08 } }],
      };
    },
    [dataKey],
  );
  return <EChart option={option} height={40} label={label} />;
}
