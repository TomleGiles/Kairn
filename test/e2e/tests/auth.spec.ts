import { expect, test } from "@playwright/test";

import { expectPage } from "./helpers";

test.describe("connexion", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("une visite anonyme mène à la connexion", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveURL(/\/login/);
    await expect(page.getByRole("heading", { name: "Connexion à Kairn" })).toBeVisible();
  });

  test("la connexion de démonstration ouvre la vue d'ensemble", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Adresse e-mail professionnelle").fill("demo@kairn.local");
    await page.getByRole("button", { name: "Continuer" }).click();
    await page.waitForURL(/\/o\/[0-9a-f-]+/);
    await expectPage(page, "Vue d'ensemble");
  });
});
