import { request, type FullConfig } from "@playwright/test";

/** Ouvre une session de démonstration une fois pour toute la suite. */
export default async function globalSetup(config: FullConfig): Promise<void> {
  const baseURL = config.projects[0]?.use.baseURL ?? "http://localhost:3000";
  const ctx = await request.newContext({ baseURL });
  const res = await ctx.post("/api/v1/auth/login", { data: { email: "demo@kairn.local" } });
  if (!res.ok()) {
    throw new Error(`demo login failed: ${res.status()} ${await res.text()}`);
  }
  await ctx.storageState({ path: ".auth/demo.json" });
  await ctx.dispose();
}
