// Revue du site vitrine (apps/site) : captures et contrôle axe, pour chaque
// langue (français à la racine, anglais sous /en/).
//   node site-review.mjs [url] [dossier]
import AxeBuilder from "@axe-core/playwright";
import { chromium } from "@playwright/test";

const base = (process.argv[2] ?? "http://127.0.0.1:3200/").replace(/\/?$/, "/");
const dir = process.argv[3] ?? "site-review";
const browser = await chromium.launch({ channel: process.env.E2E_CHANNEL || undefined });
let failures = 0;
for (const lang of [
  { code: "fr", path: "", locale: "fr-FR" },
  { code: "en", path: "en/", locale: "en-GB" },
]) {
  for (const v of [
    { name: "desktop-light", viewport: { width: 1440, height: 900 }, scheme: "light" },
    { name: "desktop-dark", viewport: { width: 1440, height: 900 }, scheme: "dark" },
    { name: "mobile-light", viewport: { width: 390, height: 844 }, scheme: "light" },
  ]) {
    // Animations réduites : les révélations au défilement sont affichées dans leur
    // état final, ce qui rend captures pleine page et contrôle de contraste fiables.
    const ctx = await browser.newContext({ viewport: v.viewport, colorScheme: v.scheme, locale: lang.locale, reducedMotion: "reduce" });
    const page = await ctx.newPage();
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto(base + lang.path, { waitUntil: "networkidle" });
    const name = `${lang.code}-${v.name}`;
    await page.screenshot({ path: `${dir}/${name}-top.png` });
    await page.screenshot({ path: `${dir}/${name}-full.png`, fullPage: true });
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    const htmlLang = await page.evaluate(() => document.documentElement.lang);
    const res = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
    const serious = res.violations.filter((x) => x.impact === "serious" || x.impact === "critical");
    const badLang = htmlLang === lang.code ? 0 : 1;
    console.log(`${name}: lang=${htmlLang}, horizontal overflow=${overflow}px, script errors=${errors.length}, axe serious=${serious.length}`);
    for (const s of serious) console.log(`  - ${s.id}: ${s.help} → ${s.nodes.slice(0, 4).map((n) => n.target.join(" ")).join(" | ")}`);
    failures += serious.length + errors.length + (overflow > 0 ? 1 : 0) + badLang;
    await ctx.close();
  }
}
await browser.close();
process.exit(failures ? 1 : 0);
