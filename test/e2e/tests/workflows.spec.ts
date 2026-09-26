import { expect, test } from "@playwright/test";

import { euroAmount, expectPage, openDemoOrg } from "./helpers";

test.describe("parcours", () => {
  test("les connecteurs de démonstration sont listés et documentés", async ({ page }) => {
    const org = await openDemoOrg(page);
    await page.goto(org + "/connectors");
    await expectPage(page, "Connecteurs");
    await expect(page.getByText(/OpenStack/).first()).toBeVisible();
    await expect(page.getByText(/Kubernetes/).first()).toBeVisible();
  });

  test("l'explorateur de coûts ventile les montants", async ({ page }) => {
    const org = await openDemoOrg(page);
    await page.goto(org + "/costs");
    await expectPage(page, "Explorateur de coûts");
    await expect(page.getByText(euroAmount).first()).toBeVisible();
  });

  test("l'export CSV des coûts est téléchargeable par l'API", async ({ page, request }) => {
    const org = await openDemoOrg(page);
    const orgId = org.split("/").pop()!;
    const today = new Date(new Date().toISOString().slice(0, 10) + "T00:00:00Z");
    const from = new Date(today.getTime() - 7 * 86_400_000);
    const q = new URLSearchParams({ from: from.toISOString(), to: today.toISOString(), format: "csv" });
    const res = await request.get(`/api/v1/orgs/${orgId}/costs/export?${q}`);
    expect(res.status()).toBe(200);
    expect(res.headers()["content-type"]).toContain("text/csv");
    const csv = await res.text();
    expect(csv.split("\n")[0]).toContain("provider");
    expect(csv.split("\n").length).toBeGreaterThan(1);
  });

  test("un budget créé par l'API apparaît dans l'interface", async ({ page, request }) => {
    const org = await openDemoOrg(page);
    const orgId = org.split("/").pop()!;
    const name = `E2E budget ${Date.now()}`;
    const created = await request.post(`/api/v1/orgs/${orgId}/budgets`, {
      data: { name, period: "monthly", amount: "1234.50" },
    });
    expect(created.status()).toBe(201);
    const budget = (await created.json()) as { id: string };
    await page.goto(org + "/budgets");
    await expectPage(page, "Budgets et alertes");
    await expect(page.getByText(name)).toBeVisible();
    const del = await request.delete(`/api/v1/orgs/${orgId}/budgets/${budget.id}`);
    expect(del.status()).toBe(204);
  });

  test("les recommandations chiffrent leurs économies", async ({ page }) => {
    const org = await openDemoOrg(page);
    await page.goto(org + "/recommendations");
    await expectPage(page, "Recommandations");
    await expect(page.getByText(euroAmount).first()).toBeVisible();
  });

  test("un agent pousse ses métriques OTLP par la passerelle", async ({ page, request, playwright, baseURL }) => {
    const org = await openDemoOrg(page);
    const orgId = org.split("/").pop()!;
    const created = await request.post(`/api/v1/orgs/${orgId}/connectors`, { data: { type: "agent", name: `E2E agent ${Date.now()}` } });
    expect(created.status()).toBe(201);
    const conn = (await created.json()) as { id: string; webhook_url: string };
    const token = conn.webhook_url.split("/").pop()!;
    // Requête sans session : seul le jeton de l'agent authentifie l'envoi.
    const agent = await playwright.request.newContext({ baseURL, storageState: { cookies: [], origins: [] } });
    const now = BigInt(Date.now()) * 1_000_000n;
    const res = await agent.post("/ingest/v1/otlp/v1/metrics", {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        resourceMetrics: [
          {
            resource: { attributes: [{ key: "host.id", value: { stringValue: "e2e-host" } }] },
            scopeMetrics: [{ metrics: [{ name: "system.cpu.utilization", gauge: { dataPoints: [{ timeUnixNano: now.toString(), asDouble: 0.4 }] } }] }],
          },
        ],
      },
    });
    expect(res.status()).toBe(200);
    const denied = await agent.post("/ingest/v1/otlp/v1/metrics", { headers: { Authorization: "Bearer wrong" }, data: {} });
    expect(denied.status()).toBe(401);
    await agent.dispose();
    expect((await request.delete(`/api/v1/orgs/${orgId}/connectors/${conn.id}`)).status()).toBe(204);
  });

  test("une autre organisation reste inaccessible", async ({ request }) => {
    const res = await request.get("/api/v1/orgs/0192f0c0-0000-7000-8000-00000000dead/costs");
    expect([403, 404]).toContain(res.status());
  });
});
