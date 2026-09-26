"use client";

import { BarChart, GraphChart, LineChart, PieChart, TreemapChart } from "echarts/charts";
import { DatasetComponent, GridComponent, LegendComponent, MarkLineComponent, TooltipComponent } from "echarts/components";
import * as echarts from "echarts/core";
import { SVGRenderer } from "echarts/renderers";
import { useEffect, useRef } from "react";

echarts.use([BarChart, LineChart, PieChart, TreemapChart, GraphChart, GridComponent, TooltipComponent, LegendComponent, DatasetComponent, MarkLineComponent, SVGRenderer]);

export type EChartOption = echarts.EChartsCoreOption;

function cssVar(name: string): string {
  if (typeof window === "undefined") return "#888";
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || "#888";
}

/** Palette issue des jetons CSS (clair / sombre). */
export function palette(): string[] {
  return Array.from({ length: 8 }, (_, i) => cssVar(`--chart-${i + 1}`));
}

/**
 * Graphique ECharts accessible : rendu SVG, description textuelle (aria-label)
 * et tableau de données de repli pour les lecteurs d'écran.
 */
export function EChart({
  option,
  height = 280,
  label,
  table,
}: {
  option: (colors: string[], text: { fg: string; muted: string; border: string }) => EChartOption;
  height?: number;
  label: string;
  table?: { headers: string[]; rows: (string | number)[][] };
}) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!ref.current) return;
    const chart = echarts.init(ref.current, undefined, { renderer: "svg" });
    const render = () => {
      const text = { fg: cssVar("--foreground"), muted: cssVar("--muted"), border: cssVar("--border") };
      chart.setOption(
        {
          textStyle: { fontFamily: "inherit", color: text.muted },
          animationDuration: 300,
          ...option(palette(), text),
        },
        true,
      );
    };
    render();
    const ro = new ResizeObserver(() => chart.resize());
    ro.observe(ref.current);
    const mo = new MutationObserver(render);
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    return () => {
      ro.disconnect();
      mo.disconnect();
      chart.dispose();
    };
  }, [option]);
  return (
    <figure className="m-0">
      <div ref={ref} style={{ height }} role="img" aria-label={label} />
      {table ? (
        <figcaption className="sr-only">
          <table>
            <caption>{label}</caption>
            <thead>
              <tr>
                {table.headers.map((h, i) => (
                  <th key={i}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {table.rows.map((r, i) => (
                <tr key={i}>
                  {r.map((c, j) => (
                    <td key={j}>{c}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </figcaption>
      ) : null}
    </figure>
  );
}
