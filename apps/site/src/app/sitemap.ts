import type { MetadataRoute } from "next";

import { homePath, locales } from "@/lib/content";
import { site } from "@/lib/site";

export const dynamic = "force-static";

// Une entrée par langue, chacune déclarant sa traduction (hreflang).
export default function sitemap(): MetadataRoute.Sitemap {
  const languages = Object.fromEntries(locales.map((l) => [l, `${site.url}${homePath[l]}`]));
  return locales.map((l) => ({
    url: `${site.url}${homePath[l]}`,
    changeFrequency: "monthly",
    priority: l === "fr" ? 1 : 0.9,
    alternates: { languages },
  }));
}
