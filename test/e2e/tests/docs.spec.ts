import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

// Site de documentation (apps/docs → /docs) : navigation, liens internes et accessibilité.
test.describe("documentation", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("aucun lien interne cassé", async ({ page, request }) => {
    const seen = new Set<string>();
    const queue = ["/docs"];
    const broken: string[] = [];
    while (queue.length) {
      const path = queue.shift()!;
      if (seen.has(path)) continue;
      seen.add(path);
      const res = await request.get(path);
      if (res.status() !== 200) {
        broken.push(`${path} → ${res.status()}`);
        continue;
      }
      if (!(res.headers()["content-type"] ?? "").includes("text/html")) continue;
      await page.goto(path);
      const hrefs = await page.$$eval("main a[href], nav a[href]", (as) => as.map((a) => a.getAttribute("href") ?? ""));
      for (const href of hrefs) {
        if (!href.startsWith("/docs")) continue;
        const target = href.split("#")[0];
        if (!seen.has(target)) queue.push(target);
      }
    }
    expect(broken).toEqual([]);
    expect(seen.size).toBeGreaterThan(25);
  });

  test("les ancres des webhooks existent", async ({ page }) => {
    await page.goto("/docs/connectors/webhooks");
    for (const id of ["gitlab", "github", "argo-cd", "flux", "alertmanager", "pagerduty", "opsgenie"]) {
      await expect(page.locator(`[id="${id}"]`).first()).toBeAttached();
    }
  });

  for (const path of ["/docs", "/docs/connectors/openstack", "/docs/guide/configuration"]) {
    test(`accessibilité ${path}`, async ({ page }) => {
      await page.goto(path);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      const res = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
      const serious = res.violations.filter((v) => v.impact === "serious" || v.impact === "critical");
      expect(serious.map((v) => `${v.id}: ${v.help} — ${v.nodes.map((n) => n.target.join(" ")).slice(0, 3).join(", ")}`)).toEqual([]);
    });
  }
});
