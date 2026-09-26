package notify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/allocation"
	"github.com/kairn-io/kairn/pkg/budget"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/secrets"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/pkg/tsdb"
)

func base64Std(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// ChannelAAD lie les secrets d'un canal à son organisation (identique à l'API).
func ChannelAAD(orgID, channelID string) []byte { return secrets.AAD(orgID, "channel", channelID) }

// WebhookAAD lie le secret d'un webhook sortant à son organisation.
func WebhookAAD(orgID, id string) []byte { return secrets.AAD(orgID, "webhook", id) }

// Engine est le moteur de notification.
type Engine struct {
	Store     store.Store
	TSDB      tsdb.TSDB
	Keyring   *secrets.Keyring
	Senders   map[string]Sender
	PublicURL string
	Log       *slog.Logger
	Now       func() time.Time
	Poster    HTTPPoster
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (e *Engine) log() *slog.Logger {
	if e.Log != nil {
		return e.Log
	}
	return slog.Default()
}

var severityRank = map[string]int{"info": 0, "warning": 1, "critical": 2}

// ruleMatches applique les filtres de configuration d'une règle.
func ruleMatches(r model.AlertRule, req AlertRequest) bool {
	if !r.Enabled || r.Kind != req.Kind {
		return false
	}
	if min := r.Config["min_severity"]; min != "" && severityRank[req.Severity] < severityRank[min] {
		return false
	}
	if min := r.Config["min_savings"]; min != "" {
		want, err1 := decimal.NewFromString(min)
		got, err2 := decimal.NewFromString(fmt.Sprint(req.Payload["savings_monthly"]))
		if err1 == nil && (err2 != nil || got.LessThan(want)) {
			return false
		}
	}
	if b := r.Config["budget_id"]; b != "" && fmt.Sprint(req.Payload["budget_id"]) != b {
		return false
	}
	if c := r.Config["check_id"]; c != "" && fmt.Sprint(req.Payload["check_id"]) != c {
		return false
	}
	return true
}

// silenced indique si une alerte est couverte par un silence actif.
func silenced(sils []model.Silence, req AlertRequest, ruleID string, now time.Time) bool {
	for _, s := range sils {
		if now.Before(s.StartsAt) || !now.Before(s.EndsAt) {
			continue
		}
		ok := true
		for k, v := range s.Matchers {
			var got string
			switch k {
			case "kind":
				got = req.Kind
			case "severity":
				got = req.Severity
			case "fingerprint":
				got = req.Fingerprint
				if strings.HasSuffix(v, "*") && strings.HasPrefix(got, strings.TrimSuffix(v, "*")) {
					continue
				}
			case "rule_id":
				got = ruleID
			}
			if got != v {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// InBusinessHours indique si t tombe dans les heures ouvrées (nil = toujours).
func InBusinessHours(b *model.BusinessHours, t time.Time) bool {
	if b == nil {
		return true
	}
	loc, err := time.LoadLocation(b.Timezone)
	if err != nil {
		return true
	}
	lt := t.In(loc)
	wd := int(lt.Weekday())
	if wd == 0 {
		wd = 7
	}
	if len(b.Days) > 0 {
		found := false
		for _, d := range b.Days {
			if d == wd {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	start, err1 := time.Parse("15:04", b.Start)
	end, err2 := time.Parse("15:04", b.End)
	if err1 != nil || err2 != nil {
		return true
	}
	mins := lt.Hour()*60 + lt.Minute()
	return mins >= start.Hour()*60+start.Minute() && mins < end.Hour()*60+end.Minute()
}

// Handle traite une demande d'alerte pour l'organisation du contexte.
func (e *Engine) Handle(ctx context.Context, req AlertRequest) error {
	orgID, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	if req.Fingerprint == "" {
		return errors.New("notify: fingerprint required")
	}
	if req.Severity == "" {
		req.Severity = "info"
	}
	now := e.now()
	org, err := e.Store.Orgs().Get(ctx, orgID)
	if err != nil {
		return fmt.Errorf("load org: %w", err)
	}
	rules, err := store.ListAll(ctx, e.Store.AlertRules(), func(r model.AlertRule) string { return r.ID }, nil)
	if err != nil {
		return err
	}
	var matched []model.AlertRule
	for _, r := range rules {
		if (req.RuleID != "" && r.ID == req.RuleID && r.Enabled) || (req.RuleID == "" && ruleMatches(r, req)) {
			matched = append(matched, r)
		}
	}
	channelSet := map[string]bool{}
	for _, id := range req.ChannelIDs {
		channelSet[id] = true
	}
	window := time.Hour
	var bh *model.BusinessHours
	var ruleID *string
	for _, r := range matched {
		for _, c := range r.ChannelIDs {
			channelSet[c] = true
		}
		if r.GroupWindowSeconds > 0 {
			window = time.Duration(r.GroupWindowSeconds) * time.Second
		}
		if r.BusinessHours != nil {
			bh = r.BusinessHours
		}
		id := r.ID
		ruleID = &id
	}
	sils, err := store.ListAll(ctx, e.Store.Silences(), func(s model.Silence) string { return s.ID }, nil)
	if err != nil {
		return err
	}
	rid := ""
	if ruleID != nil {
		rid = *ruleID
	}
	cur, err := e.Store.AlertEvents().FindActive(ctx, req.Fingerprint)
	exists := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}

	if req.Resolved {
		if !exists {
			return nil
		}
		wasNotified := cur.NotifiedAt != nil
		cur.Status, cur.LastAt = model.AlertResolved, now
		if err := e.Store.AlertEvents().Update(ctx, &cur); err != nil {
			return err
		}
		if wasNotified {
			e.deliver(ctx, org, channelSet, cur, true)
		}
		return nil
	}

	if exists {
		cur.Count++
		cur.LastAt = now
		cur.Title, cur.Body = req.Title, req.Body
		if cur.NotifiedAt != nil && now.Sub(*cur.NotifiedAt) < window {
			// Regroupé avec la notification précédente.
			return e.Store.AlertEvents().Update(ctx, &cur)
		}
	} else {
		payload := req.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		cur = model.AlertEvent{
			OrgID: orgID, RuleID: ruleID, Kind: req.Kind, Severity: req.Severity, Fingerprint: req.Fingerprint,
			Title: req.Title, Body: req.Body, Link: req.Link, Payload: payload, Status: model.AlertFiring,
			Count: 1, FirstAt: now, LastAt: now,
		}
	}
	switch {
	case silenced(sils, req, rid, now):
		cur.Status = model.AlertSilenced
	case len(channelSet) == 0:
		cur.Status = model.AlertFiring // visible dans l'UI, sans canal
	case !InBusinessHours(bh, now):
		cur.Status = model.AlertFiring // différée : envoyée par FlushDeferred
	default:
		cur.Status = model.AlertFiring
		if e.deliver(ctx, org, channelSet, cur, false) {
			cur.NotifiedAt = &now
		}
	}
	if exists {
		err = e.Store.AlertEvents().Update(ctx, &cur)
	} else {
		err = e.Store.AlertEvents().Create(ctx, &cur)
	}
	if err != nil {
		return err
	}
	if cur.Status == model.AlertFiring && cur.NotifiedAt != nil {
		e.DeliverWebhooks(ctx, "alert.fired", map[string]any{"alert": cur})
	}
	return nil
}

func (e *Engine) link(path string) string {
	if path == "" || strings.HasPrefix(path, "http") {
		return path
	}
	return strings.TrimRight(e.PublicURL, "/") + path
}

// deliver envoie l'alerte sur les canaux ; renvoie vrai si au moins un envoi a réussi.
func (e *Engine) deliver(ctx context.Context, org model.Organization, channels map[string]bool, ev model.AlertEvent, resolved bool) bool {
	fields := map[string]string{}
	for k, v := range ev.Payload {
		switch x := v.(type) {
		case string:
			fields[k] = x
		case float64:
			fields[k] = strconv.FormatFloat(x, 'f', -1, 64)
		}
	}
	msg := Message{OrgName: org.Name, Kind: ev.Kind, Severity: ev.Severity, Title: ev.Title, Body: ev.Body,
		Link: e.link(ev.Link), Fingerprint: ev.Fingerprint, Fields: fields, Resolved: resolved, At: e.now()}
	ids := make([]string, 0, len(channels))
	for id := range channels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ok := false
	for _, id := range ids {
		ch, err := e.Store.Channels().Get(ctx, id)
		if err != nil || !ch.Enabled {
			continue
		}
		if err := e.send(ctx, ch, msg); err != nil {
			e.log().Warn("notification failed", "org", org.ID, "channel", ch.ID, "kind", ch.Kind, "err", err)
			continue
		}
		ok = true
	}
	return ok
}

func (e *Engine) send(ctx context.Context, ch model.NotificationChannel, msg Message) error {
	sender, found := e.Senders[ch.Kind]
	if !found {
		return fmt.Errorf("no sender for %s", ch.Kind)
	}
	sec := map[string]string{}
	if len(ch.SecretsEnc) > 0 {
		if e.Keyring == nil {
			return errors.New("no keyring")
		}
		var err error
		if sec, err = e.Keyring.DecryptMap(ctx, ch.SecretsEnc, ChannelAAD(ch.OrgID, ch.ID)); err != nil {
			return err
		}
	}
	sctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return sender.Send(sctx, ch, sec, msg)
}

// Test envoie un message de test sur un canal de l'organisation du contexte.
func (e *Engine) Test(ctx context.Context, channelID string) error {
	orgID, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	org, err := e.Store.Orgs().Get(ctx, orgID)
	if err != nil {
		return err
	}
	ch, err := e.Store.Channels().Get(ctx, channelID)
	if err != nil {
		return err
	}
	return e.send(ctx, ch, Message{OrgName: org.Name, Kind: "test", Severity: "info", Title: "Notification de test Kairn",
		Body: "Ce canal est correctement configuré.", Link: e.link("/alerts"), Fingerprint: "test:" + channelID, At: e.now()})
}

// FlushDeferred envoie les alertes différées hors heures ouvrées quand la plage s'ouvre.
func (e *Engine) FlushDeferred(ctx context.Context) error {
	orgID, err := tenancy.OrgID(ctx)
	if err != nil {
		return err
	}
	org, err := e.Store.Orgs().Get(ctx, orgID)
	if err != nil {
		return err
	}
	evs, err := e.Store.AlertEvents().List(ctx, store.ListQuery{Limit: store.MaxLimit, Filters: map[string]string{"status": model.AlertFiring}})
	if err != nil {
		return err
	}
	now := e.now()
	for _, ev := range evs {
		if ev.NotifiedAt != nil || ev.RuleID == nil {
			continue
		}
		r, err := e.Store.AlertRules().Get(ctx, *ev.RuleID)
		if err != nil || !InBusinessHours(r.BusinessHours, now) {
			continue
		}
		chs := map[string]bool{}
		for _, c := range r.ChannelIDs {
			chs[c] = true
		}
		if e.deliver(ctx, org, chs, ev, false) {
			ev.NotifiedAt = &now
			_ = e.Store.AlertEvents().Update(ctx, &ev)
		}
	}
	return nil
}

// EvaluateBudgets déclenche les alertes de seuil réel et prévisionnel.
func (e *Engine) EvaluateBudgets(ctx context.Context) error {
	bs, err := store.ListAll(ctx, e.Store.Budgets(), func(b model.Budget) string { return b.ID }, nil)
	if err != nil || len(bs) == 0 {
		return err
	}
	nodes, err := store.ListAll(ctx, e.Store.AllocationNodes(), func(n model.AllocationNode) string { return n.ID }, nil)
	if err != nil {
		return err
	}
	tree := allocation.NewTree(nodes)
	now := e.now()
	for _, b := range bs {
		st, err := budget.Evaluate(ctx, e.TSDB, b, tree, now)
		if err != nil {
			return err
		}
		period := st.PeriodStart.Format("2006-01-02")
		scope := "l'organisation"
		if b.NodeID != nil {
			if n, ok := tree.Node(*b.NodeID); ok {
				scope = n.Name
			}
		}
		fields := map[string]any{"budget_id": b.ID, "budget": b.Amount.StringFixed(2) + " " + b.Currency,
			"actual": st.Actual.StringFixed(2) + " " + b.Currency, "forecast": st.Forecast.StringFixed(2) + " " + b.Currency}
		crossed := budget.CrossedThresholds(b.Thresholds, st.ActualPercent)
		if len(crossed) > 0 {
			th := crossed[len(crossed)-1]
			sev := "warning"
			if th >= 100 {
				sev = "critical"
			}
			req := AlertRequest{Kind: model.AlertBudgetActual, Severity: sev,
				Fingerprint: fmt.Sprintf("budget:%s:%s:actual:%d", b.ID, period, th),
				Title:       fmt.Sprintf("Budget « %s » : %d %% atteint", b.Name, th),
				Body: fmt.Sprintf("La dépense de %s atteint %s %s, soit %s %% du budget %s de %s %s.",
					scope, st.Actual.StringFixed(2), b.Currency, st.ActualPercent.String(), periodLabel(b.Period), b.Amount.StringFixed(2), b.Currency),
				Link: "/budgets", Payload: fields, ChannelIDs: b.ChannelIDs}
			if err := e.Handle(ctx, req); err != nil {
				return err
			}
		}
		if b.ForecastAlert && st.ForecastPercent.GreaterThan(decimal.NewFromInt(100)) && st.ActualPercent.LessThan(decimal.NewFromInt(100)) {
			req := AlertRequest{Kind: model.AlertBudgetForecast, Severity: "warning",
				Fingerprint: fmt.Sprintf("budget:%s:%s:forecast", b.ID, period),
				Title:       fmt.Sprintf("Budget « %s » : dépassement prévu", b.Name),
				Body: fmt.Sprintf("Au rythme actuel, %s dépensera %s %s d'ici la fin de la période (%s %% du budget).",
					scope, st.Forecast.StringFixed(2), b.Currency, st.ForecastPercent.String()),
				Link: "/budgets", Payload: fields, ChannelIDs: b.ChannelIDs}
			if err := e.Handle(ctx, req); err != nil {
				return err
			}
		}
	}
	return nil
}

func periodLabel(p string) string {
	switch p {
	case budget.Quarterly:
		return "trimestriel"
	case budget.Yearly:
		return "annuel"
	}
	return "mensuel"
}

// DeliverWebhooks pousse un événement vers les webhooks clients abonnés (signés, 3 tentatives).
func (e *Engine) DeliverWebhooks(ctx context.Context, event string, data map[string]any) {
	subs, err := store.ListAll(ctx, e.Store.Webhooks(), func(w model.WebhookSubscription) string { return w.ID }, nil)
	if err != nil {
		return
	}
	for _, s := range subs {
		if !s.Enabled || !contains(s.Events, event) {
			continue
		}
		secret, err := e.Keyring.Decrypt(ctx, s.SecretEnc, WebhookAAD(s.OrgID, s.ID))
		if err != nil {
			e.log().Warn("webhook secret unreadable", "webhook", s.ID)
			continue
		}
		body, _ := json.Marshal(map[string]any{"event": event, "org_id": s.OrgID, "at": e.now(), "data": data})
		headers := map[string]string{"X-Kairn-Event": event, "X-Kairn-Signature": Sign(string(secret), body), "X-Kairn-Delivery": fmt.Sprint(e.now().UnixNano())}
		var lastErr error
		for attempt := 0; attempt < 3; attempt++ {
			if lastErr = e.Poster.post(ctx, s.URL, json.RawMessage(body), headers); lastErr == nil {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
			}
		}
		if lastErr != nil {
			e.log().Warn("webhook delivery failed", "webhook", s.ID, "event", event, "err", lastErr)
		}
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// DefaultSenders construit les expéditeurs standard.
func DefaultSenders(email Email, poster HTTPPoster) map[string]Sender {
	return map[string]Sender{
		model.ChannelEmail:      email,
		model.ChannelSlack:      Slack{poster},
		model.ChannelTeams:      Teams{poster},
		model.ChannelMattermost: Mattermost{poster},
		model.ChannelWebhook:    Webhook{poster},
		model.ChannelPagerDuty:  PagerDuty{HTTPPoster: poster},
	}
}
