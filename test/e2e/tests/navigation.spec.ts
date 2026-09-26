import { expect, test } from "@playwright/test";

import { euroAmount, expectPage, openDemoOrg } from "./helpers";

// Chaque écran de l'organisation de démonstration s'affiche avec ses données.
const screens: { path: string; title: string | RegExp }[] = [
  { path: "", title: "Vue d'ensemble" },
  { path: "/costs", title: "Explorateur de coûts" },
  { path: "/resources", title: "Inventaire" },
  { path: "/topology", title: "Topologie" },
  { path: "/efficiency", title: "Coût × usage" },
  { path: "/allocation", title: "Allocation" },
  { path: "/recommendations", title: "Recommandations" },
  { path: "/anomalies", title: "Anomalies" },
  { path: "/budgets", title: "Budgets et alertes" },
  { path: "/forecast", title: "Prévisions" },
  { path: "/reports", title: "Rapports" },
  { path: "/uptime", title: "Uptime et pages de statut" },
  { path: "/connectors", title: "Connecteurs" },
  { path: "/settings", title: "Paramètres" },
];

test.describe("navigation", () => {
  for (const s of screens) {
    test(`écran ${s.path || "/"}`, async ({ page }) => {
      const errors: string[] = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const org = await openDemoOrg(page);
      await page.goto(org + s.path);
      await expectPage(page, s.title);
      expect(errors, "no uncaught script error").toEqual([]);
    });
  }

  test("la vue d'ensemble affiche des montants en euros", async ({ page }) => {
    await openDemoOrg(page);
    await expectPage(page, "Vue d'ensemble");
    await expect(page.getByText(euroAmount).first()).toBeVisible();
  });

  test("le menu latéral mène aux écrans", async ({ page }) => {
    await openDemoOrg(page);
    await page.getByRole("link", { name: "Explorateur de coûts" }).first().click();
    await expectPage(page, "Explorateur de coûts");
    await page.getByRole("link", { name: "Recommandations" }).first().click();
    await expectPage(page, "Recommandations");
  });
});
