package api

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

type checkBody struct {
	Name            string   `json:"name" required:"true" minLength:"1" maxLength:"120"`
	Kind            string   `json:"kind" required:"true" enum:"http,tcp,icmp"`
	Target          string   `json:"target" required:"true" maxLength:"2048" doc:"URL (http), hôte:port (tcp) ou hôte (icmp)"`
	IntervalSeconds int      `json:"interval_seconds,omitempty" minimum:"0" maximum:"3600"`
	TimeoutMS       int      `json:"timeout_ms,omitempty" minimum:"0" maximum:"60000"`
	Regions         []string `json:"regions,omitempty"`
	ExpectedStatus  int      `json:"expected_status,omitempty" minimum:"0" maximum:"599"`
	Keyword         string   `json:"keyword,omitempty" maxLength:"200"`
	NodeID          *string  `json:"node_id,omitempty" doc:"Service (nœud d'allocation) concerné"`
	FailThreshold   int      `json:"fail_threshold,omitempty" minimum:"0" maximum:"10"`
	Enabled         *bool    `json:"enabled,omitempty"`
}

type statusPageBody struct {
	Slug     string            `json:"slug" required:"true" pattern:"^[a-z0-9][a-z0-9-]{1,62}$"`
	Title    string            `json:"title" required:"true" minLength:"1" maxLength:"120"`
	Public   *bool             `json:"public,omitempty"`
	CheckIDs []string          `json:"check_ids" required:"true"`
	Branding map[string]string `json:"branding,omitempty" doc:"logo_url, primary_color"`
}

type incidentBody struct {
	Title   string  `json:"title" required:"true" minLength:"1" maxLength:"200"`
	CheckID *string `json:"check_id,omitempty"`
	Status  string  `json:"status,omitempty" enum:"open,resolved"`
	Message string  `json:"message,omitempty" maxLength:"2000"`
}

// Regions est la liste des régions de sonde disponibles.
var Regions = []string{"eu-west-gra", "eu-west-par", "eu-central-waw", "eu-west-rbx"}

func validateTarget(kind, target string) error {
	switch kind {
	case "http":
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return invalid("http target must be an http(s) URL")
		}
	case "tcp":
		if _, _, err := net.SplitHostPort(target); err != nil {
			return invalid("tcp target must be host:port")
		}
	case "icmp":
		if strings.ContainsAny(target, "/: ") {
			return invalid("icmp target must be a host name or IP")
		}
	}
	return nil
}

// checkStatus résume l'état d'une sonde.
type checkStatus struct {
	CheckID      string          `json:"check_id"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind"`
	Up           *bool           `json:"up"`
	LastCheckAt  *time.Time      `json:"last_check_at,omitempty"`
	LatencyMS    float64         `json:"latency_ms"`
	Uptime24h    float64         `json:"uptime_24h"`
	Uptime7d     float64         `json:"uptime_7d"`
	Uptime30d    float64         `json:"uptime_30d"`
	Daily        []dailyUptime   `json:"daily"`
	RegionStatus map[string]bool `json:"regions"`
}

type dailyUptime struct {
	Day    time.Time `json:"day"`
	Uptime float64   `json:"uptime"`
	Checks int       `json:"checks"`
}

func (s *Server) checkStatuses(ctx context.Context, checks []model.UptimeCheck, days int) ([]checkStatus, error) {
	now := s.now()
	from := tsdb.TruncDay(now).AddDate(0, 0, -days+1)
	idsList := make([]string, 0, len(checks))
	for _, c := range checks {
		idsList = append(idsList, c.ID)
	}
	var results []model.UptimeResult
	if len(idsList) > 0 {
		var err error
		results, err = s.TSDB.QueryUptime(ctx, tsdb.UptimeQuery{CheckIDs: idsList, From: from, To: now.Add(time.Minute)})
		if err != nil {
			return nil, err
		}
	}
	byCheck := map[string][]model.UptimeResult{}
	for _, r := range results {
		byCheck[r.CheckID] = append(byCheck[r.CheckID], r)
	}
	ratio := func(rs []model.UptimeResult, since time.Time) float64 {
		up, n := 0, 0
		for _, r := range rs {
			if r.TS.Before(since) {
				continue
			}
			n++
			if r.Up {
				up++
			}
		}
		if n == 0 {
			return 1
		}
		return round3(float64(up) / float64(n) * 100)
	}
	out := make([]checkStatus, 0, len(checks))
	for _, c := range checks {
		rs := byCheck[c.ID]
		st := checkStatus{CheckID: c.ID, Name: c.Name, Kind: c.Kind, RegionStatus: map[string]bool{},
			Uptime24h: ratio(rs, now.Add(-24*time.Hour)), Uptime7d: ratio(rs, now.AddDate(0, 0, -7)), Uptime30d: ratio(rs, now.AddDate(0, 0, -30))}
		latest := map[string]model.UptimeResult{}
		lat, latN := 0.0, 0
		for _, r := range rs {
			if cur, ok := latest[r.Region]; !ok || r.TS.After(cur.TS) {
				latest[r.Region] = r
			}
			if r.TS.After(now.Add(-time.Hour)) && r.Up {
				lat += r.LatencyMS
				latN++
			}
		}
		if latN > 0 {
			st.LatencyMS = round3(lat / float64(latN))
		}
		if len(latest) > 0 {
			down := 0
			var last time.Time
			for region, r := range latest {
				st.RegionStatus[region] = r.Up
				if !r.Up {
					down++
				}
				if r.TS.After(last) {
					last = r.TS
				}
			}
			threshold := c.FailThreshold
			if threshold <= 0 {
				threshold = 1
			}
			up := down < threshold
			st.Up, st.LastCheckAt = &up, &last
		}
		for d := from; d.Before(now); d = d.AddDate(0, 0, 1) {
			end := d.AddDate(0, 0, 1)
			upN, n := 0, 0
			for _, r := range rs {
				if !r.TS.Before(d) && r.TS.Before(end) {
					n++
					if r.Up {
						upN++
					}
				}
			}
			du := dailyUptime{Day: d, Checks: n, Uptime: 100}
			if n > 0 {
				du.Uptime = round3(float64(upN) / float64(n) * 100)
			}
			st.Daily = append(st.Daily, du)
		}
		out = append(out, st)
	}
	return out, nil
}

type publicStatus struct {
	Title       string            `json:"title"`
	Branding    map[string]string `json:"branding"`
	Overall     string            `json:"overall" doc:"operational | degraded | outage"`
	Components  []checkStatus     `json:"components"`
	Incidents   []model.Incident  `json:"incidents"`
	GeneratedAt time.Time         `json:"generated_at"`
}

func (s *Server) registerUptime() {
	tag := "Uptime"
	registerCRUD(s, crudSpec[model.UptimeCheck, checkBody]{
		Tag: tag, Path: "/uptime/checks", Name: "uptime-check", Plural: "uptime-checks", Summary: "sondes d'uptime",
		Read: auth.PermCostsRead, Write: auth.PermUptimeManage, Feature: plans.FeatureUptime, Repo: func() store.CRUD[model.UptimeCheck] { return s.Store.UptimeChecks() },
		ID: func(c *model.UptimeCheck) string { return c.ID }, Filters: []string{"kind"},
		Apply: func(ctx context.Context, a access, b *checkBody, c *model.UptimeCheck, isNew bool) error {
			if err := validateTarget(b.Kind, b.Target); err != nil {
				return err
			}
			regions := b.Regions
			if len(regions) == 0 {
				regions = []string{Regions[0], Regions[1]}
			}
			for _, r := range regions {
				if !containsStr(Regions, r) {
					return invalid("unknown region " + r)
				}
			}
			if b.NodeID != nil {
				if err := s.nodeExists(ctx, *b.NodeID); err != nil {
					return err
				}
			}
			*c = model.UptimeCheck{ID: c.ID, OrgID: a.Org.ID, Name: b.Name, Kind: b.Kind, Target: b.Target, IntervalSeconds: b.IntervalSeconds,
				TimeoutMS: b.TimeoutMS, Regions: regions, ExpectedStatus: b.ExpectedStatus, Keyword: b.Keyword, NodeID: b.NodeID,
				FailThreshold: b.FailThreshold, Enabled: b.Enabled == nil || *b.Enabled, CreatedAt: c.CreatedAt}
			if c.IntervalSeconds < 30 {
				c.IntervalSeconds = 60
			}
			if c.TimeoutMS == 0 {
				c.TimeoutMS = 10000
			}
			if c.ExpectedStatus == 0 && c.Kind == "http" {
				c.ExpectedStatus = 200
			}
			if c.FailThreshold == 0 {
				c.FailThreshold = 2
				if len(regions) < 2 {
					c.FailThreshold = 1
				}
			}
			return nil
		},
	})
	huma.Register(s.API, huma.Operation{OperationID: "get-uptime-summary", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/uptime/summary", Tags: []string{tag},
		Summary: "État et disponibilité des sondes (24 h, 7 j, 30 j)"},
		func(ctx context.Context, in *OrgPath) (*Out[[]checkStatus], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureUptime)
			if err != nil {
				return nil, err
			}
			checks, err := store.ListAll(ctx, s.Store.UptimeChecks(), func(c model.UptimeCheck) string { return c.ID }, nil)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			st, err := s.checkStatuses(ctx, checks, 30)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(st), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "list-uptime-results", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/uptime/checks/{id}/results", Tags: []string{tag},
		Summary: "Résultats bruts d'une sonde"},
		func(ctx context.Context, in *struct {
			IDPath
			From time.Time `query:"from"`
			To   time.Time `query:"to"`
		}) (*Out[[]model.UptimeResult], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, plans.FeatureUptime)
			if err != nil {
				return nil, err
			}
			if _, err := s.Store.UptimeChecks().Get(ctx, in.ID); err != nil {
				return nil, s.fail(ctx, err)
			}
			from, to := in.From, in.To
			if to.IsZero() {
				to = s.now().Add(time.Minute)
			}
			if from.IsZero() {
				from = to.Add(-24 * time.Hour)
			}
			rs, err := s.TSDB.QueryUptime(ctx, tsdb.UptimeQuery{CheckIDs: []string{in.ID}, From: from, To: to, Limit: 5000})
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if rs == nil {
				rs = []model.UptimeResult{}
			}
			return out(rs), nil
		})

	registerCRUD(s, crudSpec[model.StatusPage, statusPageBody]{
		Tag: tag, Path: "/status-pages", Name: "status-page", Plural: "status-pages", Summary: "pages de statut",
		Read: auth.PermCostsRead, Write: auth.PermUptimeManage, Feature: plans.FeatureUptime,
		Repo: func() store.CRUD[model.StatusPage] { return s.Store.StatusPages() },
		ID:   func(p *model.StatusPage) string { return p.ID },
		Apply: func(ctx context.Context, a access, b *statusPageBody, p *model.StatusPage, isNew bool) error {
			for _, id := range b.CheckIDs {
				if _, err := s.Store.UptimeChecks().Get(ctx, id); err != nil {
					return invalid("unknown check " + id)
				}
			}
			p.OrgID, p.Slug, p.Title, p.CheckIDs, p.Branding = a.Org.ID, b.Slug, b.Title, b.CheckIDs, b.Branding
			p.Public = b.Public == nil || *b.Public
			if p.Branding == nil {
				p.Branding = map[string]string{}
			}
			return nil
		},
	})
	type tokenOut struct {
		AccessToken string `json:"access_token" doc:"Affiché une seule fois ; l'ancien jeton est invalidé"`
		URL         string `json:"url"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "rotate-status-page-token", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/status-pages/{id}/token", Tags: []string{tag},
		Summary: "Génère le jeton d'accès d'une page de statut privée"},
		func(ctx context.Context, in *IDPath) (*Out[tokenOut], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermUptimeManage, plans.FeatureUptime)
			if err != nil {
				return nil, err
			}
			p, err := s.Store.StatusPages().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			tok, err := auth.RandomString(24)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			h := auth.HashSecret(tok)
			p.AccessHash = &h
			if err := s.Store.StatusPages().Update(ctx, &p); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "status-page.token.rotate", "status_page", p.ID, nil)
			return out(tokenOut{AccessToken: tok, URL: strings.TrimRight(s.Config.PublicURL, "/") + "/status/" + p.Slug + "?token=" + tok}), nil
		})

	registerCRUD(s, crudSpec[model.Incident, incidentBody]{
		Tag: tag, Path: "/incidents", Name: "incident", Plural: "incidents", Summary: "incidents",
		Read: auth.PermCostsRead, Write: auth.PermUptimeManage, Filters: []string{"status"},
		Repo: func() store.CRUD[model.Incident] { return s.Store.Incidents() },
		ID:   func(i *model.Incident) string { return i.ID },
		Apply: func(ctx context.Context, a access, b *incidentBody, i *model.Incident, isNew bool) error {
			now := s.now()
			status := b.Status
			if status == "" {
				status = "open"
			}
			if isNew {
				*i = model.Incident{OrgID: a.Org.ID, CheckID: b.CheckID, Title: b.Title, Status: status, Source: "manual", StartedAt: now, Updates: []model.IncidentUpdate{}}
			} else {
				i.Title = b.Title
				i.Status = status
			}
			if status == "resolved" && i.ResolvedAt == nil {
				i.ResolvedAt = &now
			}
			if b.Message != "" || isNew {
				msg := b.Message
				if msg == "" {
					msg = b.Title
				}
				i.Updates = append(i.Updates, model.IncidentUpdate{At: now, Status: status, Message: msg})
			}
			return nil
		},
	})

	huma.Register(s.API, huma.Operation{OperationID: "get-public-status-page", Method: http.MethodGet, Path: Prefix + "/public/status/{slug}", Tags: []string{tag},
		Summary: "Page de statut publique (ou privée avec jeton)", Security: []map[string][]string{}},
		func(ctx context.Context, in *struct {
			Slug  string `path:"slug"`
			Token string `query:"token"`
		}) (*Out[publicStatus], error) {
			orgID, pageID, public, hash, err := s.Store.System().FindStatusPage(ctx, in.Slug)
			if err != nil {
				return nil, huma.Error404NotFound("status page not found")
			}
			if !public && (in.Token == "" || subtle.ConstantTimeCompare([]byte(auth.HashSecret(in.Token)), []byte(hash)) != 1) {
				return nil, huma.Error404NotFound("status page not found")
			}
			octx := tenancy.WithOrg(ctx, orgID)
			p, err := s.Store.StatusPages().Get(octx, pageID)
			if err != nil {
				return nil, huma.Error404NotFound("status page not found")
			}
			var checks []model.UptimeCheck
			for _, id := range p.CheckIDs {
				if c, err := s.Store.UptimeChecks().Get(octx, id); err == nil {
					checks = append(checks, c)
				}
			}
			st, err := s.checkStatuses(octx, checks, 90)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			res := publicStatus{Title: p.Title, Branding: p.Branding, Components: st, Overall: "operational", GeneratedAt: s.now(), Incidents: []model.Incident{}}
			for _, c := range st {
				if c.Up != nil && !*c.Up {
					res.Overall = "outage"
				} else if c.Uptime24h < 99 && res.Overall == "operational" {
					res.Overall = "degraded"
				}
			}
			incs, err := s.Store.Incidents().List(octx, store.ListQuery{Limit: 100})
			if err == nil {
				cutoff := s.now().AddDate(0, 0, -14)
				for _, i := range incs {
					if i.CheckID != nil && !containsStr(p.CheckIDs, *i.CheckID) {
						continue
					}
					if i.Status == "open" || i.StartedAt.After(cutoff) {
						res.Incidents = append(res.Incidents, i)
					}
				}
				sort.Slice(res.Incidents, func(a, b int) bool { return res.Incidents[a].StartedAt.After(res.Incidents[b].StartedAt) })
			}
			return out(res), nil
		})
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
