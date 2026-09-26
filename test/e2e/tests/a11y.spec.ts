import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

import { expectPage, openDemoOrg } from "./helpers";

// RGAA / WCAG 2.1 AA : aucune violation grave ou critique détectée
// automatiquement sur les écrans principaux (l'audit manuel reste nécessaire).
const pages: { path: string; title: string }[] = [
  { path: "", title: "Vue d'ensemble" },
  { path: "/costs", title: "Explorateur de coûts" },
  { path: "/recommendations", title: "Recommandations" },
  { path: "/budgets", title: "Budgets et alertes" },
  { path: "/connectors", title: "Connecteurs" },
];

test.describe("accessibilité", () => {
  test("page de connexion", async ({ browser }) => {
    const ctx = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    const page = await ctx.newPage();
    await page.goto("/login");
    const res = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
    const serious = res.violations.filter((v) => v.impact === "serious" || v.impact === "critical");
    expect(serious.map((v) => `${v.id}: ${v.help} (${v.nodes.length})`)).toEqual([]);
    await ctx.close();
  });

  test("vue d'ensemble d'une organisation encore vide", async ({ page, request }) => {
    const created = await request.post("/api/v1/orgs", { data: { name: `E2E vide ${Date.now()}` } });
    expect(created.status()).toBe(201);
    const org = (await created.json()) as { id: string };
    await page.goto(`/o/${org.id}`);
    await expectPage(page, "Vue d'ensemble");
    await page.waitForLoadState("networkidle");
    const res = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).exclude("canvas").analyze();
    const serious = res.violations.filter((v) => v.impact === "serious" || v.impact === "critical");
    expect(serious.map((v) => `${v.id}: ${v.help} — ${v.nodes.map((n) => n.target.join(" ")).slice(0, 3).join(", ")}`)).toEqual([]);
  });

  for (const p of pages) {
    test(`écran ${p.path || "/"}`, async ({ page }) => {
      const org = await openDemoOrg(page);
      await page.goto(org + p.path);
      await expectPage(page, p.title);
      await page.waitForLoadState("networkidle");
      const res = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).exclude("canvas").analyze();
      const serious = res.violations.filter((v) => v.impact === "serious" || v.impact === "critical");
      expect(serious.map((v) => `${v.id}: ${v.help} — ${v.nodes.map((n) => n.target.join(" ")).slice(0, 3).join(", ")}`)).toEqual([]);
    });
  }
});
