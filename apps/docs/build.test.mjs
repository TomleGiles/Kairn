import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

import { build, renderPage, rewriteLink, routeOf, slug } from "./build.mjs";

test("routes des pages", () => {
  assert.equal(routeOf("docs/README.md"), "/docs");
  assert.equal(routeOf("docs/connectors/README.md"), "/docs/connectors");
  assert.equal(routeOf("docs/guide/demarrage.md"), "/docs/guide/demarrage");
  assert.equal(routeOf("CHANGELOG.md"), "/docs/changelog");
  assert.equal(routeOf("docs/api/openapi.yaml"), null);
  assert.equal(routeOf("services/api/main.go"), null);
});

test("ancres stables", () => {
  assert.equal(slug("Argo CD"), "argo-cd");
  assert.equal(slug("Répartition du coût des nodes Kubernetes"), "repartition-du-cout-des-nodes-kubernetes");
});

test("réécriture des liens", () => {
  assert.equal(rewriteLink("../adr/0006-grilles-tarifaires-publiques.md", "docs/guide/modele-de-cout.md", ""), "/docs/adr/0006-grilles-tarifaires-publiques");
  assert.equal(rewriteLink("webhooks.md#gitlab", "docs/connectors/README.md", ""), "/docs/connectors/webhooks#gitlab");
  assert.equal(rewriteLink("../CHANGELOG.md", "docs/README.md", ""), "/docs/changelog");
  assert.equal(rewriteLink("api/openapi.yaml", "docs/README.md", ""), "/docs/api/openapi.yaml");
  assert.equal(rewriteLink("https://example.com", "docs/README.md", ""), "https://example.com");
  // Fichier du dépôt hors documentation : lien vers le dépôt s'il est connu, sinon texte.
  assert.equal(rewriteLink("../../deploy/postgres/init-roles.sql", "docs/guide/deploiement.md", ""), null);
  assert.equal(
    rewriteLink("../../deploy/postgres/init-roles.sql", "docs/guide/deploiement.md", "https://git.example/kairn"),
    "https://git.example/kairn/blob/main/deploy/postgres/init-roles.sql",
  );
});

test("rendu d'une page", () => {
  const { title, html } = renderPage("# Titre\n\n## Section Deux\n\n[ADR](../adr/README.md) et [code](../../pkg/x.go)\n\n| a | b |\n|---|---|\n| 1 | 2 |\n", "docs/guide/x.md", "");
  assert.equal(title, "Titre");
  assert.match(html, /<h2 id="section-deux">/);
  assert.match(html, /href="\/docs\/adr"/);
  assert.match(html, /<span class="repo-path">code<\/span>/);
  assert.match(html, /<div class="table-wrap"><table>/);
});

test("construction du site complet", () => {
  const out = mkdtempSync(join(tmpdir(), "kairn-docs-"));
  const n = build({ out, repoURL: "" });
  assert.ok(n >= 20, `pages: ${n}`);
  for (const f of ["index.html", "guide/demarrage.html", "connectors.html", "connectors/openstack.html", "adr.html", "changelog.html", "style.css", "api/openapi.yaml"]) {
    assert.ok(existsSync(join(out, f)), `missing ${f}`);
  }
  const page = readFileSync(join(out, "connectors", "webhooks.html"), "utf8");
  assert.match(page, /id="gitlab"/);
  assert.match(page, /<html lang="fr">/);
  assert.doesNotMatch(page, /href="[^"]*\.md[#"]/, "no link may still point to a Markdown file");
});
