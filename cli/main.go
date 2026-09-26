// Commande kairn : CLI de Kairn (M-12). Elle n'utilise que l'API publique
// /api/v1, avec un jeton d'API scoppé (mêmes droits que dans l'interface).
//
//	kairn login --url https://kairn.example --token kairn_…
//	kairn orgs | kairn use <org>
//	kairn summary
//	kairn costs --from 2026-09-01 --group-by provider,cost_type
//	kairn recommendations [--status open] | kairn recommendations accept <id>
//	kairn anomalies | kairn budgets | kairn resources --type compute.instance
//	kairn connectors | kairn connectors sync <id>
//	kairn export costs --from 2026-08-01 --to 2026-09-01 > costs.csv
//	kairn ask "Pourquoi les coûts de l'équipe Data ont-ils augmenté ?"
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var version = "dev"

// out est la sortie standard (remplaçable dans les tests).
var out io.Writer = os.Stdout

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "kairn:", err)
		os.Exit(1)
	}
}

const usage = `Usage : kairn <commande> [options]

Commandes :
  login            enregistre l'URL et le jeton d'API (--url, --token)
  orgs             liste les organisations accessibles
  use <org>        choisit l'organisation courante
  summary          synthèse du mois (dépense, prévision, économies)
  costs            coûts agrégés (--from, --to, --group-by, --granularity, --filter)
  recommendations  recommandations (--status, --type) ; accept|dismiss|postpone <id>
  anomalies        anomalies de coût détectées
  budgets          état des budgets
  resources        inventaire (--type, --q)
  connectors       connecteurs ; sync <id> pour lancer une synchronisation
  export costs     export CSV des coûts (--from, --to, --group-by)
  ask <question>   question à l'assistant IA
  version          version de la CLI

Options communes : -o table|json|csv (format de sortie)
Variables : KAIRN_URL, KAIRN_TOKEN, KAIRN_ORG, KAIRN_CONFIG`

func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(out, usage)
		return nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version":
		fmt.Fprintln(out, "kairn", version)
		return nil
	case "login":
		return login(ctx, rest)
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cmd == "use" {
		if len(rest) != 1 {
			return errors.New("usage: kairn use <org-id>")
		}
		cfg.Org = rest[0]
		return saveConfig(cfg)
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	switch cmd {
	case "orgs":
		return orgs(ctx, c, rest)
	case "summary":
		return summary(ctx, c, rest)
	case "costs":
		return costs(ctx, c, rest)
	case "recommendations", "recos":
		return recommendations(ctx, c, rest)
	case "anomalies":
		return anomalies(ctx, c, rest)
	case "budgets":
		return budgets(ctx, c, rest)
	case "resources":
		return resources(ctx, c, rest)
	case "connectors":
		return connectors(ctx, c, rest)
	case "export":
		return export(ctx, c, rest)
	case "ask":
		return ask(ctx, c, rest)
	}
	return fmt.Errorf("unknown command %q (kairn help)", cmd)
}

func flags(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	format := fs.String("o", "table", "format de sortie : table, json, csv")
	return fs, format
}

func login(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	u := fs.String("url", "", "URL de Kairn")
	tok := fs.String("token", "", "jeton d'API (kairn_…)")
	org := fs.String("org", "", "organisation par défaut")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tok == "" {
		*tok = os.Getenv("KAIRN_TOKEN")
	}
	parsed, err := url.Parse(*u)
	if err != nil || (parsed.Scheme != "https" && !strings.HasPrefix(parsed.Host, "localhost")) || parsed.Host == "" {
		return errors.New("--url must be an https:// URL (http:// only for localhost)")
	}
	cfg := Config{URL: strings.TrimRight(*u, "/"), Token: *tok, Org: *org}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	var me struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
		Memberships []struct {
			OrgID string `json:"org_id"`
		} `json:"memberships"`
		Permissions map[string][]string `json:"permissions"`
	}
	if err := c.JSON(ctx, http.MethodGet, "/me", nil, nil, &me); err != nil {
		return fmt.Errorf("token check failed: %w", err)
	}
	if cfg.Org == "" && len(me.Permissions) == 1 {
		for id := range me.Permissions {
			cfg.Org = id
		}
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "Connecté à %s", cfg.URL)
	if cfg.Org != "" {
		fmt.Fprintf(out, " (organisation %s)", cfg.Org)
	}
	fmt.Fprintln(out)
	return nil
}

func orgs(ctx context.Context, c *Client, args []string) error {
	fs, format := flags("orgs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var me struct {
		Memberships []struct {
			OrgID   string `json:"org_id"`
			OrgName string `json:"org_name"`
			Role    string `json:"role"`
			Plan    string `json:"plan"`
		} `json:"memberships"`
		Permissions map[string][]string `json:"permissions"`
	}
	if err := c.JSON(ctx, http.MethodGet, "/me", nil, nil, &me); err != nil {
		return err
	}
	t := table{head: []string{"", "ID", "NOM", "RÔLE", "PLAN"}}
	for _, m := range me.Memberships {
		cur := ""
		if m.OrgID == c.cfg.Org {
			cur = "*"
		}
		t.add(cur, m.OrgID, m.OrgName, m.Role, m.Plan)
	}
	if len(me.Memberships) == 0 {
		for id := range me.Permissions { // jeton d'API : une seule organisation
			t.add("*", id, "", "", "")
		}
	}
	return t.print(*format)
}

// dates convertit --from/--to (AAAA-MM-JJ) en instants RFC 3339 ; défaut : mois courant.
func dates(from, to string) (string, string, error) {
	now := time.Now().UTC()
	f := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	var err error
	if from != "" {
		if f, err = time.Parse("2006-01-02", from); err != nil {
			return "", "", fmt.Errorf("--from: expected YYYY-MM-DD")
		}
	}
	if to != "" {
		if t, err = time.Parse("2006-01-02", to); err != nil {
			return "", "", fmt.Errorf("--to: expected YYYY-MM-DD")
		}
	}
	return f.Format(time.RFC3339), t.Format(time.RFC3339), nil
}

func split(s string) []string {
	var outv []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			outv = append(outv, x)
		}
	}
	return outv
}

func summary(ctx context.Context, c *Client, args []string) error {
	fs, format := flags("summary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var s map[string]any
	if err := c.JSON(ctx, http.MethodGet, "/orgs/{org}/costs/summary", nil, nil, &s); err != nil {
		return err
	}
	if *format == "json" {
		return printJSON(s)
	}
	cur := fmt.Sprint(s["currency"])
	t := table{head: []string{"INDICATEUR", "VALEUR"}}
	t.add("Dépense du mois à date", fmt.Sprint(s["month_to_date"])+" "+cur)
	t.add("Même période le mois précédent", fmt.Sprint(s["previous_month_to_date"])+" "+cur)
	t.add("Évolution", fmt.Sprint(s["change_percent"])+" %")
	t.add("Prévision de fin de mois", fmt.Sprint(s["forecast_month_end"])+" "+cur)
	t.add("Économies potentielles / mois", fmt.Sprint(s["potential_savings_monthly"])+" "+cur)
	t.add("Couverture d'allocation", fmt.Sprint(s["allocation_coverage_percent"])+" %")
	t.add("Anomalies ouvertes", fmt.Sprint(s["open_anomalies"]))
	return t.print(*format)
}

func costs(ctx context.Context, c *Client, args []string) error {
	fs, format := flags("costs")
	from := fs.String("from", "", "début (AAAA-MM-JJ, inclus)")
	to := fs.String("to", "", "fin (AAAA-MM-JJ, exclue)")
	group := fs.String("group-by", "provider", "dimensions séparées par des virgules (provider, cost_type, allocation_node_id, label:team…)")
	gran := fs.String("granularity", "total", "day, week, month ou total")
	filter := fs.String("filter", "", "filtres dimension:valeur séparés par des virgules")
	if err := fs.Parse(args); err != nil {
		return err
	}
	f, t, err := dates(*from, *to)
	if err != nil {
		return err
	}
	q := url.Values{"from": {f}, "to": {t}, "granularity": {*gran}}
	for _, g := range split(*group) {
		q.Add("group_by", g)
	}
	for _, x := range split(*filter) {
		q.Add("filter", x)
	}
	var res struct {
		Currency string   `json:"currency"`
		Total    string   `json:"total"`
		GroupBy  []string `json:"group_by"`
		Rows     []struct {
			Period string            `json:"period"`
			Keys   map[string]string `json:"keys"`
			Amount string            `json:"amount"`
		} `json:"rows"`
	}
	if err := c.JSON(ctx, http.MethodGet, "/orgs/{org}/costs", q, nil, &res); err != nil {
		return err
	}
	if *format == "json" {
		return printJSON(res)
	}
	names := map[string]string{}
	for _, g := range res.GroupBy {
		if g == "allocation_node_id" {
			names = nodeNames(ctx, c)
		}
	}
	head := []string{}
	if *gran != "total" {
		head = append(head, "PÉRIODE")
	}
	for _, g := range res.GroupBy {
		head = append(head, strings.ToUpper(g))
	}
	head = append(head, "MONTANT ("+res.Currency+")")
	tb := table{head: head}
	for _, r := range res.Rows {
		var row []string
		if *gran != "total" {
			row = append(row, prefix(r.Period, 10))
		}
		for _, g := range res.GroupBy {
			v := r.Keys[g]
			if n, ok := names[v]; ok && g == "allocation_node_id" {
				v = n
			}
			row = append(row, v)
		}
		tb.add(append(row, money(r.Amount, *format))...)
	}
	if *format == "table" {
		tb.footer = "Total : " + money(res.Total, *format) + " " + res.Currency
	}
	return tb.print(*format)
}

func recommendations(ctx context.Context, c *Client, args []string) error {
	if len(args) >= 2 && (args[0] == "accept" || args[0] == "dismiss" || args[0] == "postpone" || args[0] == "applied") {
		status := map[string]string{"accept": "accepted", "dismiss": "dismissed", "postpone": "postponed", "applied": "applied"}[args[0]]
		fs := flag.NewFlagSet("recommendations", flag.ContinueOnError)
		reason := fs.String("reason", "", "motif (obligatoire pour dismiss)")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		body := map[string]any{"status": status, "reason": *reason}
		if status == "postponed" {
			body["postponed_until"] = time.Now().UTC().AddDate(0, 0, 30).Format(time.RFC3339)
		}
		if err := c.JSON(ctx, http.MethodPost, "/orgs/{org}/recommendations/"+url.PathEscape(args[1])+"/status", nil, body, nil); err != nil {
			return err
		}
		fmt.Fprintf(out, "Recommandation %s : %s\n", args[1], status)
		return nil
	}
	fs, format := flags("recommendations")
	status := fs.String("status", "open", "open, accepted, postponed, dismissed, applied")
	typ := fs.String("type", "", "type de recommandation")
	limit := fs.Int("limit", 20, "nombre maximum")
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := url.Values{"status": {*status}, "limit": {fmt.Sprint(*limit)}}
	if *typ != "" {
		q.Set("type", *typ)
	}
	var res struct {
		Items []struct {
			ID             string `json:"id"`
			Type           string `json:"type"`
			Title          string `json:"title"`
			SavingsMonthly string `json:"savings_monthly"`
			Currency       string `json:"currency"`
			Risk           string `json:"risk"`
			Remediation    struct {
				CLI string `json:"cli"`
			} `json:"remediation"`
		} `json:"items"`
	}
	if err := c.JSON(ctx, http.MethodGet, "/orgs/{org}/recommendations", q, nil, &res); err != nil {
		return err
	}
	if *format == "json" {
		return printJSON(res.Items)
	}
	t := table{head: []string{"ID", "TYPE", "RECOMMANDATION", "ÉCONOMIE/MOIS", "RISQUE"}}
	for _, r := range res.Items {
		t.add(r.ID, r.Type, r.Title, r.SavingsMonthly+" "+r.Currency, r.Risk)
	}
	return t.print(*format)
}

func anomalies(ctx context.Context, c *Client, args []string) error {
	fs, format := flags("anomalies")
	status := fs.String("status", "", "open, acknowledged, resolved")
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := url.Values{}
	if *status != "" {
		q.Set("status", *status)
	}
	var res []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Severity    string `json:"severity"`
		WindowStart string `json:"window_start"`
		Expected    string `json:"expected"`
		Actual      string `json:"actual"`
		Currency    string `json:"currency"`
		Explanation string `json:"explanation"`
	}
	if err := c.JSON(ctx, http.MethodGet, "/orgs/{org}/anomalies", q, nil, &res); err != nil {
		return err
	}
	if *format == "json" {
		return printJSON(res)
	}
	t := table{head: []string{"DÉBUT", "SÉVÉRITÉ", "ANOMALIE", "ATTENDU", "OBSERVÉ"}}
	for _, a := range res {
		t.add(prefix(a.WindowStart, 10), a.Severity, a.Title, a.Expected+" "+a.Currency, a.Actual+" "+a.Currency)
	}
	return t.print(*format)
}

func budgets(ctx context.Context, c *Client, args []string) error {
	fs, format := flags("budgets")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var res []struct {
		Budget struct {
			Name   string `json:"name"`
			Period string `json:"period"`
			Amount string `json:"amount"`
		} `json:"budget"`
		Actual          string `json:"actual"`
		Forecast        string `json:"forecast"`
		ForecastPercent string `json:"forecast_percent"`
		Currency        string `json:"currency"`
	}
	if err := c.JSON(ctx, http.MethodGet, "/orgs/{org}/budgets-status", nil, nil, &res); err != nil {
		return err
	}
	if *format == "json" {
		return printJSON(res)
	}
	t := table{head: []string{"BUDGET", "PÉRIODE", "MONTANT", "RÉEL", "PRÉVISION", "PRÉVISION %"}}
	for _, b := range res {
		t.add(b.Budget.Name, b.Budget.Period, b.Budget.Amount, b.Actual, b.Forecast, b.ForecastPercent+" %")
	}
	return t.print(*format)
}

func resources(ctx context.Context, c *Client, args []string) error {
	fs, format := flags("resources")
	typ := fs.String("type", "", "type de ressource (compute.instance, k8s.workload…)")
	query := fs.String("q", "", "recherche par nom")
	limit := fs.Int("limit", 50, "nombre maximum")
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := url.Values{"limit": {fmt.Sprint(*limit)}}
	if *typ != "" {
		q.Set("type", *typ)
	}
	if *query != "" {
		q.Set("q", *query)
	}
	var res struct {
		Items []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Type     string `json:"type"`
			Provider string `json:"provider"`
			Region   string `json:"region"`
			Cost30d  string `json:"cost_30d"`
			Currency string `json:"currency"`
		} `json:"items"`
	}
	if err := c.JSON(ctx, http.MethodGet, "/orgs/{org}/resources", q, nil, &res); err != nil {
		return err
	}
	if *format == "json" {
		return printJSON(res.Items)
	}
	t := table{head: []string{"NOM", "TYPE", "FOURNISSEUR", "RÉGION", "COÛT 30 J", "ID"}}
	for _, r := range res.Items {
		t.add(r.Name, r.Type, r.Provider, r.Region, r.Cost30d+" "+r.Currency, r.ID)
	}
	return t.print(*format)
}

func connectors(ctx context.Context, c *Client, args []string) error {
	if len(args) == 2 && args[0] == "sync" {
		if err := c.JSON(ctx, http.MethodPost, "/orgs/{org}/connectors/"+url.PathEscape(args[1])+"/sync", nil, map[string]any{}, nil); err != nil {
			return err
		}
		fmt.Fprintln(out, "Synchronisation demandée.")
		return nil
	}
	fs, format := flags("connectors")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var res struct {
		Items []struct {
			ID         string `json:"id"`
			Type       string `json:"type"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			LastSyncAt string `json:"last_sync_at"`
		} `json:"items"`
	}
	if err := c.JSON(ctx, http.MethodGet, "/orgs/{org}/connectors", nil, nil, &res); err != nil {
		return err
	}
	if *format == "json" {
		return printJSON(res.Items)
	}
	t := table{head: []string{"ID", "TYPE", "NOM", "ÉTAT", "DERNIÈRE SYNCHRO"}}
	for _, x := range res.Items {
		t.add(x.ID, x.Type, x.Name, x.Status, x.LastSyncAt)
	}
	return t.print(*format)
}

func export(ctx context.Context, c *Client, args []string) error {
	if len(args) == 0 || args[0] != "costs" {
		return errors.New("usage: kairn export costs [--from] [--to] [--group-by]")
	}
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	from := fs.String("from", "", "début (AAAA-MM-JJ)")
	to := fs.String("to", "", "fin (AAAA-MM-JJ, exclue)")
	group := fs.String("group-by", "allocation_node_id,provider,cost_type", "dimensions")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	f, t, err := dates(*from, *to)
	if err != nil {
		return err
	}
	q := url.Values{"from": {f}, "to": {t}, "format": {"csv"}}
	for _, g := range split(*group) {
		q.Add("group_by", g)
	}
	return c.Raw(ctx, "/orgs/{org}/costs/export", q, out)
}

func ask(ctx context.Context, c *Client, args []string) error {
	if len(args) == 0 {
		return errors.New(`usage: kairn ask "question"`)
	}
	lang := "fr"
	if strings.HasPrefix(strings.ToLower(os.Getenv("LANG")), "en") {
		lang = "en"
	}
	body := map[string]any{"locale": lang, "messages": []map[string]string{{"role": "user", "content": strings.Join(args, " ")}}}
	var sources []string
	var failure string
	err := c.Stream(ctx, "/orgs/{org}/assistant/chat", body, func(ev map[string]any) {
		switch ev["type"] {
		case "text":
			fmt.Fprint(out, ev["text"])
		case "reset":
			fmt.Fprintln(out, "\n[réponse corrigée]")
		case "warning":
			fmt.Fprintln(out, "\n⚠", ev["message"])
		case "error":
			failure = fmt.Sprint(ev["message"])
		case "sources":
			if list, ok := ev["sources"].([]any); ok {
				for _, s := range list {
					sources = append(sources, fmt.Sprint(s))
				}
			}
		}
	})
	fmt.Fprintln(out)
	if err != nil {
		return err
	}
	if failure != "" {
		return errors.New(failure)
	}
	if len(sources) > 0 {
		sort.Strings(sources)
		fmt.Fprintln(out, "\nSources :")
		for _, s := range sources {
			fmt.Fprintln(out, "  -", s)
		}
	}
	return nil
}

// money arrondit un montant décimal au centime pour l'affichage (le CSV garde la précision complète).
func money(v, format string) string {
	if format == "csv" {
		return v
	}
	d, err := decimal.NewFromString(v)
	if err != nil {
		return v
	}
	return d.StringFixed(2)
}

// nodeNames associe les identifiants des nœuds d'allocation à leur nom.
func nodeNames(ctx context.Context, c *Client) map[string]string {
	names := map[string]string{"unallocated": "(non alloué)"}
	var res struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := c.JSON(ctx, http.MethodGet, "/orgs/{org}/allocation/nodes", url.Values{"limit": {"500"}}, nil, &res); err == nil {
		for _, n := range res.Items {
			names[n.ID] = n.Name
		}
	}
	return names
}

// prefix renvoie les n premiers octets de s (sans paniquer si s est plus court).
func prefix(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}
