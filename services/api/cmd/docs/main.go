// Commande docs : génère la documentation des connecteurs (docs/connectors/*.md)
// à partir de leur déclaration (connector.TypeInfo : ressources, fréquence,
// permissions minimales, champs), complétée par les notes rédigées dans
// notes/<page>.md. La page est ainsi toujours alignée sur le code.
//
//	go run ./services/api/cmd/docs docs/connectors
package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/kairn-io/kairn/connectors/all"
	"github.com/kairn-io/kairn/pkg/connector"
)

//go:embed notes/*.md
var notes embed.FS

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: docs <output-dir>")
		os.Exit(2)
	}
	n, err := generate(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "docs:", err)
		os.Exit(1)
	}
	fmt.Printf("Documentation des connecteurs : %d pages → %s\n", n, os.Args[1])
}

// page regroupe les types documentés dans un même fichier.
type page struct {
	name  string
	types []connector.TypeInfo
}

// pages répartit les types par page d'après leur DocsURL (/docs/connectors/<page>#<ancre>).
func pages() ([]page, error) {
	by := map[string]*page{}
	for _, t := range connector.Types() {
		if t.DocsURL == "" {
			continue // connecteurs de démonstration
		}
		rest, ok := strings.CutPrefix(t.DocsURL, "/docs/connectors/")
		if !ok {
			return nil, fmt.Errorf("%s: unexpected docs URL %q", t.Type, t.DocsURL)
		}
		name, _, _ := strings.Cut(rest, "#")
		if by[name] == nil {
			by[name] = &page{name: name}
		}
		by[name].types = append(by[name].types, t)
	}
	out := make([]page, 0, len(by))
	for _, p := range by {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

func generate(dir string) (int, error) {
	ps, err := pages()
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return 0, err
	}
	var index strings.Builder
	index.WriteString("# Connecteurs\n\n" + generatedNotice("") +
		"Tous les connecteurs sont en **lecture seule** : ils n'effectuent aucune écriture sur l'infrastructure du client. " +
		"Les credentials sont chiffrés (enveloppe, KMS ou Vault) et ne sont jamais journalisés.\n\n" +
		"| Connecteur | Catégorie | Fréquence | Métriques | Facturation réelle |\n|---|---|---|---|---|\n")
	for _, p := range ps {
		note, err := notes.ReadFile("notes/" + p.name + ".md")
		if err != nil {
			return 0, fmt.Errorf("missing notes/%s.md: %w", p.name, err)
		}
		body := render(p, string(note))
		if err := os.WriteFile(filepath.Join(dir, p.name+".md"), []byte(body), 0o600); err != nil {
			return 0, err
		}
		for _, t := range p.types {
			link := p.name + ".md"
			if _, anchor, ok := strings.Cut(t.DocsURL, "#"); ok {
				link += "#" + anchor
			}
			freq := interval(t.DefaultInterval)
			if t.Category == connector.CategoryEvents {
				freq = "push"
			}
			fmt.Fprintf(&index, "| [%s](%s) | %s | %s | %s | %s |\n", t.DisplayName, link, t.Category, freq, yes(t.Metrics), yes(t.Billing))
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(index.String()), 0o600); err != nil {
		return 0, err
	}
	return len(ps), nil
}

func generatedNotice(name string) string {
	src := "services/api/cmd/docs"
	if name != "" {
		src += "/notes/" + name + ".md"
	}
	return "> Page générée par `make gen` à partir des déclarations des connecteurs. Pour la modifier, éditer `" + src + "` ou le connecteur.\n\n"
}

// render produit la page : en-tête, fiche générée de chaque type, puis notes.
// Une page à un seul type place les notes après la fiche ; une page multi-types
// (webhooks) insère la fiche de chaque type dans la section de notes de même
// ancre (« <!-- type:gitlab --> »).
func render(p page, note string) string {
	var b strings.Builder
	title, rest := splitTitle(note)
	b.WriteString(title + "\n\n" + generatedNotice(p.name))
	if len(p.types) == 1 {
		b.WriteString(card(p.types[0], "##"))
		b.WriteString(rest)
		return ensureNL(b.String())
	}
	for _, t := range p.types {
		marker := "<!-- type:" + t.Type + " -->"
		if !strings.Contains(rest, marker) {
			rest += "\n## " + t.DisplayName + "\n\n" + marker + "\n"
		}
		rest = strings.Replace(rest, marker, card(t, "###"), 1)
	}
	b.WriteString(rest)
	return ensureNL(b.String())
}

func splitTitle(note string) (string, string) {
	note = strings.TrimLeft(note, "\n")
	title, rest, _ := strings.Cut(note, "\n")
	return strings.TrimSpace(title), strings.TrimLeft(rest, "\n")
}

func ensureNL(s string) string { return strings.TrimRight(s, "\n") + "\n" }

// card est la fiche technique d'un type de connecteur.
func card(t connector.TypeInfo, h string) string {
	var b strings.Builder
	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Type | `%s` |\n| Nom | %s |\n| Catégorie | %s |\n| Fournisseur | `%s` |\n", t.Type, t.DisplayName, t.Category, t.Provider)
	freq := interval(t.DefaultInterval)
	if t.Category == connector.CategoryEvents {
		freq = "— (réception en push)"
	}
	fmt.Fprintf(&b, "| Fréquence de synchronisation | %s |\n", freq)
	res := "—"
	if len(t.Resources) > 0 {
		res = "`" + strings.Join(t.Resources, "`, `") + "`"
	}
	fmt.Fprintf(&b, "| Ressources | %s |\n| Métriques d'utilisation | %s |\n| Facturation réelle | %s |\n| Webhook entrant | %s |\n\n", res, yes(t.Metrics), yes(t.Billing), yes(t.Webhook))
	if len(t.Permissions) > 0 {
		b.WriteString(h + " Permissions minimales\n\n| Portée | Usage | Obligatoire |\n|---|---|---|\n")
		for _, p := range t.Permissions {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", cell(p.Scope), cell(p.Description), yes(!p.Optional))
		}
		b.WriteString("\n")
	}
	if len(t.Fields) > 0 {
		b.WriteString(h + " Configuration\n\n| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |\n|---|---|---|---|---|---|\n")
		for _, f := range t.Fields {
			def := ""
			if f.Default != "" {
				def = "`" + f.Default + "`"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s |\n", f.Name, cell(f.Label), yes(f.Required), yes(f.Secret), def, cell(f.Help))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func cell(s string) string { return strings.ReplaceAll(s, "|", "\\|") }

func yes(v bool) string {
	if v {
		return "oui"
	}
	return "non"
}

func interval(d time.Duration) string {
	switch {
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		return fmt.Sprintf("%d j", d/(24*time.Hour))
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%d h", d/time.Hour)
	default:
		return fmt.Sprintf("%d min", d/time.Minute)
	}
}
