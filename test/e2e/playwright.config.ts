import { defineConfig, devices } from "@playwright/test";

/**
 * Tests de bout en bout sur l'instance de démonstration (KAIRN_MODE=demo) :
 * API sur :8080, interface sur :3000. Les serveurs déjà lancés sont réutilisés
 * (CI), sinon Playwright les démarre.
 *
 * E2E_CHANNEL=msedge ou chrome utilise un navigateur installé localement au
 * lieu du Chromium de Playwright.
 */
const baseURL = process.env.E2E_BASE_URL ?? "http://localhost:3000";
const channel = process.env.E2E_CHANNEL;

export default defineConfig({
  testDir: "./tests",
  timeout: 60_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  globalSetup: "./global-setup.ts",
  use: {
    baseURL,
    storageState: ".auth/demo.json",
    locale: "fr-FR",
    timezoneId: "Europe/Paris",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "desktop", use: { ...devices["Desktop Chrome"], ...(channel ? { channel } : {}) } }],
  webServer: [
    {
      command: "go run ./services/api",
      cwd: "../..",
      env: { KAIRN_MODE: "demo", KAIRN_LOG_LEVEL: "warn" },
      url: "http://localhost:8080/healthz",
      reuseExistingServer: true,
      timeout: 180_000,
    },
    {
      command: "npm run dev",
      cwd: "../../apps/web",
      url: `${baseURL}/login`,
      reuseExistingServer: true,
      timeout: 180_000,
    },
  ],
});
