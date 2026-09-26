import { expect, type Page } from "@playwright/test";

/** Ouvre l'organisation de démonstration et renvoie son URL de base (/o/<id>). */
export async function openDemoOrg(page: Page): Promise<string> {
  await page.goto("/");
  await page.waitForURL(/\/o\/[0-9a-f-]+/);
  const match = /\/o\/[0-9a-f-]+/.exec(page.url());
  expect(match, "redirected to an organization").not.toBeNull();
  return match![0];
}

/** Vérifie qu'une page s'affiche sans erreur (titre visible, pas de page d'erreur). */
export async function expectPage(page: Page, title: string | RegExp): Promise<void> {
  await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();
  await expect(page.getByText("Une erreur est survenue")).toHaveCount(0);
}

/** Montant affiché en euros (format français, ex. « 1 234,56 € »). */
export const euroAmount = /\d[\d\s ]*,\d{2}\s?€/;
