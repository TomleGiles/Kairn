// Site de documentation Kairn : convertit docs/**/*.md (et le CHANGELOG) en
// pages HTML statiques, publiées dans apps/web/public/docs et servies par
// l'interface sous /docs (même origine, aucun service supplémentaire).
//
//   node apps/docs/build.mjs [--out <dossier>] [--repo https://github.com/org/kairn]
//
// Les liens entre pages Markdown deviennent des routes /docs/… ; les liens vers
// le reste du dépôt pointent vers --repo (DOCS_REPO_URL) s'il est fourni.
import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

import { Marked } from "marked";

const here = dirname(fileURLToPath(import.meta.url));
export const ROOT = resolve(here, "..", "..");

/** Identifiant d'ancre stable (« Argo CD » → « argo-cd »). */
export function slug(text) {
  return text
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/<[^>]+>/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function escapeHtml(s) {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

/** Route publique d'un fichier Markdown du dépôt (null s'il n'est pas publié). */
export function routeOf(repoPath) {
  const p = repoPath.split(sep).join("/");
  if (p === "CHANGELOG.md") return "/docs/changelog";
  if (!p.startsWith("docs/") || !p.endsWith(".md") || p.startsWith("docs/api/")) return null;
  let r = p.slice("docs/".length, -".md".length);
  if (r === "README") return "/docs";
  if (r.endsWith("/README")) r = r.slice(0, -"/README".length);
  return "/docs/" + r;
}

/** Réécrit un lien d'une page source vers une route du site ou le dépôt. */
export function rewriteLink(href, fromRepoPath, repoURL) {
  if (/^[a-z]+:/i.test(href) || href.startsWith("#") || href.startsWith("/")) return href;
  const [path, anchor] = href.split("#");
  const target = relative(ROOT, resolve(ROOT, dirname(fromRepoPath), path)).split(sep).join("/");
  const route = routeOf(target);
  const suffix = anchor ? "#" + anchor : "";
  if (route) return route + suffix;
  if (target.startsWith("docs/api/")) return "/" + target;
  return repoURL ? `${repoURL.replace(/\/$/, "")}/blob/main/${target}${suffix}` : null;
}

function listMarkdown(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) out.push(...listMarkdown(full));
    else if (name.endsWith(".md")) out.push(full);
  }
  return out;
}

/** Convertit une page ; renvoie son titre (premier titre de niveau 1) et son HTML. */
export function renderPage(markdown, repoPath, repoURL) {
  let title = "";
  const marked = new Marked({
    gfm: true,
    renderer: {
      heading({ tokens, depth, text }) {
        const inner = this.parser.parseInline(tokens);
        if (depth === 1 && !title) title = text.replace(/<[^>]+>/g, "");
        const id = slug(text);
        return `<h${depth} id="${id}">${inner}${depth > 1 ? ` <a class="anchor" href="#${id}" aria-label="Lien vers cette section">#</a>` : ""}</h${depth}>\n`;
      },
      link({ href, title: t, tokens }) {
        const inner = this.parser.parseInline(tokens);
        const target = rewriteLink(href, repoPath, repoURL);
        if (target === null) return `<span class="repo-path">${inner}</span>`;
        const external = /^https?:/i.test(target);
        return `<a href="${escapeHtml(target)}"${t ? ` title="${escapeHtml(t)}"` : ""}${external ? ' rel="noopener"' : ""}>${inner}</a>`;
      },
    },
    hooks: {
      // Tableaux défilants sur petit écran.
      postprocess(html) {
        return html.replace(/<table>/g, '<div class="table-wrap"><table>').replace(/<\/table>/g, "</table></div>");
      },
    },
  });
  const html = marked.parse(markdown);
  return { title: title || "Kairn", html };
}

const SECTIONS = [
  { title: "Guides", dir: "docs/guide" },
  { title: "Connecteurs", dir: "docs/connectors" },
  { title: "Décisions d'architecture", dir: "docs/adr" },
];

function nav(pages, current) {
  const link = (route, label) =>
    `<li><a href="${route}"${route === current ? ' aria-current="page"' : ""}>${escapeHtml(label)}</a></li>`;
  let out = `<ul>${link("/docs", "Accueil")}</ul>`;
  for (const s of SECTIONS) {
    const items = pages.filter((p) => p.repoPath.startsWith(s.dir + "/")).sort((a, b) => {
      const ai = a.repoPath.endsWith("README.md") ? 0 : 1;
      const bi = b.repoPath.endsWith("README.md") ? 0 : 1;
      return ai - bi || a.repoPath.localeCompare(b.repoPath);
    });
    if (!items.length) continue;
    out += `<h2>${escapeHtml(s.title)}</h2><ul>${items.map((p) => link(p.route, p.title)).join("")}</ul>`;
  }
  out += `<h2>Références</h2><ul>${link("/docs/changelog", "Journal des modifications")}<li><a href="/api/v1/docs">API interactive (OpenAPI)</a></li></ul>`;
  return out;
}

const STYLE = `:root{--bg:#ffffff;--fg:#0f172a;--muted:#475569;--border:#e2e8f0;--surface:#f8fafc;--brand:#0f766e;--code:#f1f5f9}
@media (prefers-color-scheme:dark){:root{--bg:#0b1120;--fg:#e2e8f0;--muted:#94a3b8;--border:#1e293b;--surface:#0f172a;--brand:#2dd4bf;--code:#1e293b}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--fg);font:16px/1.65 system-ui,-apple-system,"Segoe UI",sans-serif}
a{color:var(--brand)}.skip{position:absolute;left:-999px}.skip:focus{left:1rem;top:1rem;background:var(--bg);padding:.5rem;z-index:2}
.layout{display:grid;grid-template-columns:280px minmax(0,1fr);min-height:100vh}
nav{border-right:1px solid var(--border);background:var(--surface);padding:1.25rem;position:sticky;top:0;height:100vh;overflow:auto;font-size:.9rem}
nav .brand{font-weight:700;font-size:1.1rem;color:var(--fg);text-decoration:none}nav h2{font-size:.72rem;text-transform:uppercase;letter-spacing:.06em;color:var(--muted);margin:1.25rem 0 .4rem}
nav ul{list-style:none;margin:0;padding:0}nav li a{display:block;padding:.2rem .4rem;border-radius:.3rem;color:var(--fg);text-decoration:none}
nav li a:hover,nav li a[aria-current=page]{background:var(--border)}
main{padding:2rem 3rem;max-width:960px}h1{font-size:2rem;line-height:1.2}h2{margin-top:2.2rem;border-bottom:1px solid var(--border);padding-bottom:.3rem}
.anchor{opacity:0;text-decoration:none;font-weight:400}h2:hover .anchor,h3:hover .anchor{opacity:.6}
code{background:var(--code);padding:.1rem .3rem;border-radius:.25rem;font-size:.88em}pre{background:var(--code);padding:1rem;border-radius:.5rem;overflow:auto}pre code{padding:0;background:none}
.table-wrap{overflow-x:auto}table{border-collapse:collapse;width:100%;font-size:.92rem;margin:1rem 0}th,td{border:1px solid var(--border);padding:.45rem .6rem;text-align:left;vertical-align:top}th{background:var(--surface)}
blockquote{margin:1rem 0;padding:.5rem 1rem;border-left:4px solid var(--brand);background:var(--surface);color:var(--muted)}
.repo-path{font-family:ui-monospace,monospace;font-size:.88em}
@media (max-width:860px){.layout{grid-template-columns:1fr}nav{position:static;height:auto;border-right:0;border-bottom:1px solid var(--border)}main{padding:1.25rem}}`;

function template(page, pages) {
  return `<!doctype html>
<html lang="fr">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(page.title)} — Documentation Kairn</title>
<link rel="stylesheet" href="/docs/style.css">
</head>
<body>
<a class="skip" href="#contenu">Aller au contenu</a>
<div class="layout">
<nav aria-label="Documentation"><a class="brand" href="/docs">Kairn · Documentation</a>${nav(pages, page.route)}</nav>
<main id="contenu">
${page.html}
</main>
</div>
</body>
</html>
`;
}

/** Construit le site ; renvoie le nombre de pages. */
export function build({ out, repoURL }) {
  const files = [...listMarkdown(join(ROOT, "docs")), join(ROOT, "CHANGELOG.md")].filter((f) => existsSync(f));
  const pages = [];
  for (const file of files) {
    const repoPath = relative(ROOT, file).split(sep).join("/");
    const route = routeOf(repoPath);
    if (!route) continue;
    const { title, html } = renderPage(readFileSync(file, "utf8"), repoPath, repoURL);
    pages.push({ repoPath, route, title, html });
  }
  rmSync(out, { recursive: true, force: true });
  mkdirSync(out, { recursive: true });
  for (const page of pages) {
    const rel = page.route === "/docs" ? "index.html" : page.route.slice("/docs/".length) + ".html";
    const dest = join(out, rel);
    mkdirSync(dirname(dest), { recursive: true });
    writeFileSync(dest, template(page, pages));
  }
  writeFileSync(join(out, "style.css"), STYLE);
  if (existsSync(join(ROOT, "docs", "api"))) cpSync(join(ROOT, "docs", "api"), join(out, "api"), { recursive: true });
  return pages.length;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  const opt = (name, def) => {
    const i = args.indexOf(name);
    return i >= 0 ? args[i + 1] : def;
  };
  const out = resolve(opt("--out", join(ROOT, "apps", "web", "public", "docs")));
  const n = build({ out, repoURL: opt("--repo", process.env.DOCS_REPO_URL ?? "") });
  console.log(`Documentation : ${n} pages → ${relative(ROOT, out) || out}`);
}
