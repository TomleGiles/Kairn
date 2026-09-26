// Revue du site vitrine (apps/site) : captures et contrôle axe.
//   node site-review.mjs [url] [dossier]
import AxeBuilder from "@axe-core/playwright";
import { chromium } from "@playwright/test";

const url = process.argv[2] ?? "http://127.0.0.1:3200/";
const dir = process.argv[3] ?? "site-review";
const browser = await chromium.launch({ channel: process.env.E2E_CHANNEL || undefined });
let failures = 0;
for (const v of [
  { name: "desktop-light", viewport: { width: 1440, height: 900 }, scheme: "light" },
  { name: "desktop-dark", viewport: { width: 1440, height: 900 }, scheme: "dark" },
  { name: "mobile-light", viewport: { width: 390, height: 844 }, scheme: "light" },
]) {
  const ctx = await browser.newContext({ viewport: v.viewport, colorScheme: v.scheme, locale: "fr-FR" });
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(url, { waitUntil: "networkidle" });
  await page.screenshot({ path: `${dir}/${v.name}-top.png` });
  await page.screenshot({ path: `${dir}/${v.name}-full.png`, fullPage: true });
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  const res = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
  const serious = res.violations.filter((x) => x.impact === "serious" || x.impact === "critical");
  console.log(`${v.name}: horizontal overflow=${overflow}px, script errors=${errors.length}, axe serious=${serious.length}`);
  for (const s of serious) console.log(`  - ${s.id}: ${s.help} → ${s.nodes.slice(0, 4).map((n) => n.target.join(" ")).join(" | ")}`);
  failures += serious.length + errors.length + (overflow > 0 ? 1 : 0);
  await ctx.close();
}
await browser.close();
process.exit(failures ? 1 : 0);
