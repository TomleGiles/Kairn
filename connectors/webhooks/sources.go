package webhooks

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
)

func init() {
	for _, src := range []source{gitlab, github, argocd, flux, alertmanager, pagerduty, opsgenie} {
		register(src)
	}
}

// deployment construit un événement de déploiement ; « service » sert à la résolution de ressource.
func deployment(ts, title, service, namespace, version, source, author, env string) connector.Event {
	p := map[string]any{"service": service, "version": version, "source": source}
	if namespace != "" {
		p["namespace"] = namespace
	}
	if author != "" {
		p["author"] = author
	}
	if env != "" {
		p["environment"] = env
	}
	return connector.Event{Kind: model.EventDeployment, TS: parseTS(ts), Title: title, Payload: p}
}

// ---------------------------------------------------------------- GitLab

var gitlab = source{
	typ: "gitlab", name: "GitLab (déploiements)", provider: "gitlab", docs: "/docs/connectors/webhooks#gitlab",
	secretHelp: "Jeton secret du webhook GitLab (en-tête X-Gitlab-Token) ; cocher « Deployment events »",
	verify:     verifyToken("x-gitlab-token"),
	parse: func(_ map[string]string, body []byte) ([]connector.Event, error) {
		var p struct {
			ObjectKind      string `json:"object_kind"`
			Status          string `json:"status"`
			StatusChangedAt string `json:"status_changed_at"`
			Environment     string `json:"environment"`
			Ref             string `json:"ref"`
			ShortSHA        string `json:"short_sha"`
			Project         struct {
				Name              string `json:"name"`
				PathWithNamespace string `json:"path_with_namespace"`
			} `json:"project"`
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}
		if p.ObjectKind != "deployment" || p.Status != "success" {
			return nil, nil
		}
		version := p.Ref
		if p.ShortSHA != "" {
			version += "@" + p.ShortSHA
		}
		title := fmt.Sprintf("%s déployé en %s (%s)", p.Project.Name, p.Environment, version)
		return []connector.Event{deployment(p.StatusChangedAt, title, p.Project.Name, "", version, "gitlab", p.User.Username, p.Environment)}, nil
	},
}

// ---------------------------------------------------------------- GitHub

var github = source{
	typ: "github", name: "GitHub (déploiements)", provider: "github", docs: "/docs/connectors/webhooks#github",
	secretHelp: "Secret du webhook GitHub (signature X-Hub-Signature-256) ; événement « Deployment statuses »",
	verify:     verifyHMAC("x-hub-signature-256"),
	parse: func(h map[string]string, body []byte) ([]connector.Event, error) {
		if h["x-github-event"] != "deployment_status" {
			return nil, nil
		}
		var p struct {
			DeploymentStatus struct {
				State       string `json:"state"`
				CreatedAt   string `json:"created_at"`
				Environment string `json:"environment"`
			} `json:"deployment_status"`
			Deployment struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			} `json:"deployment"`
			Repository struct {
				Name string `json:"name"`
			} `json:"repository"`
			Sender struct {
				Login string `json:"login"`
			} `json:"sender"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}
		if p.DeploymentStatus.State != "success" {
			return nil, nil
		}
		version := p.Deployment.Ref + "@" + shortSHA(p.Deployment.SHA)
		title := fmt.Sprintf("%s déployé en %s (%s)", p.Repository.Name, p.DeploymentStatus.Environment, version)
		return []connector.Event{deployment(p.DeploymentStatus.CreatedAt, title, p.Repository.Name, "", version, "github", p.Sender.Login,
			p.DeploymentStatus.Environment)}, nil
	},
}

// ---------------------------------------------------------------- Argo CD

// Argo CD Notifications : service webhook avec l'en-tête X-Webhook-Secret et le
// modèle JSON documenté (docs/connectors/webhooks.md#argo-cd).
var argocd = source{
	typ: "argocd", name: "Argo CD (synchronisations)", provider: "argocd", docs: "/docs/connectors/webhooks#argo-cd",
	secretHelp: "Valeur de l'en-tête X-Webhook-Secret configuré dans argocd-notifications-cm",
	verify:     verifyToken("x-webhook-secret"),
	parse: func(_ map[string]string, body []byte) ([]connector.Event, error) {
		var p struct {
			App        string   `json:"app"`
			Namespace  string   `json:"namespace"`
			Revision   string   `json:"revision"`
			SyncStatus string   `json:"sync_status"`
			Health     string   `json:"health"`
			Phase      string   `json:"phase"`
			Timestamp  string   `json:"timestamp"`
			Images     []string `json:"images"`
			Author     string   `json:"author"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}
		if p.App == "" || (p.Phase != "" && p.Phase != "Succeeded") {
			return nil, nil
		}
		version := shortSHA(p.Revision)
		if len(p.Images) > 0 {
			img := p.Images[0]
			if i := strings.LastIndex(img, ":"); i > strings.LastIndex(img, "/") {
				version = img[i+1:]
			}
		}
		title := fmt.Sprintf("%s synchronisé par Argo CD (%s)", p.App, version)
		ev := deployment(p.Timestamp, title, p.App, p.Namespace, version, "argocd", p.Author, "")
		ev.Payload["revision"] = p.Revision
		return []connector.Event{ev}, nil
	},
}

// ---------------------------------------------------------------- Flux

var flux = source{
	typ: "flux", name: "Flux CD (réconciliations)", provider: "flux", docs: "/docs/connectors/webhooks#flux",
	secretHelp: "Secret du fournisseur « generic-hmac » du notification-controller (en-tête X-Signature)",
	verify:     verifyHMAC("x-signature"),
	parse: func(_ map[string]string, body []byte) ([]connector.Event, error) {
		var p struct {
			InvolvedObject struct {
				Kind      string `json:"kind"`
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"involvedObject"`
			Severity  string            `json:"severity"`
			Timestamp string            `json:"timestamp"`
			Message   string            `json:"message"`
			Reason    string            `json:"reason"`
			Metadata  map[string]string `json:"metadata"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}
		obj := p.InvolvedObject
		switch p.Reason {
		case "ReconciliationSucceeded", "UpgradeSucceeded", "InstallSucceeded":
			rev := p.Metadata["revision"]
			title := fmt.Sprintf("%s/%s réconcilié par Flux (%s)", obj.Kind, obj.Name, shortSHA(rev))
			return []connector.Event{deployment(p.Timestamp, title, obj.Name, obj.Namespace, rev, "flux", "", "")}, nil
		}
		if p.Severity == "error" {
			return []connector.Event{{Kind: model.EventK8s, TS: parseTS(p.Timestamp), Title: fmt.Sprintf("Flux %s %s/%s : %s", p.Reason, obj.Kind, obj.Name, p.Message),
				Payload: map[string]any{"service": obj.Name, "namespace": obj.Namespace, "reason": p.Reason, "source": "flux"}}}, nil
		}
		return nil, nil
	},
}

// ---------------------------------------------------------------- Alertmanager

var alertmanager = source{
	typ: "alertmanager", name: "Prometheus Alertmanager (incidents)", provider: "alertmanager", docs: "/docs/connectors/webhooks#alertmanager",
	secretHelp: "Jeton transmis par http_config.authorization du receiver (Authorization: Bearer)",
	verify:     verifyBearer,
	parse: func(_ map[string]string, body []byte) ([]connector.Event, error) {
		var p struct {
			Alerts []struct {
				Status      string            `json:"status"`
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
				StartsAt    string            `json:"startsAt"`
				EndsAt      string            `json:"endsAt"`
				Fingerprint string            `json:"fingerprint"`
			} `json:"alerts"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}
		var out []connector.Event
		for _, a := range p.Alerts {
			title := a.Annotations["summary"]
			if title == "" {
				title = a.Labels["alertname"]
			}
			ts := a.StartsAt
			status := "open"
			if a.Status == "resolved" {
				ts, status = a.EndsAt, "resolved"
			}
			service := a.Labels["deployment"]
			for _, k := range []string{"service", "app", "job"} {
				if service == "" {
					service = a.Labels[k]
				}
			}
			out = append(out, connector.Event{Kind: model.EventIncident, TS: parseTS(ts), Title: title, Payload: map[string]any{
				"status": status, "incident_key": a.Fingerprint, "severity": a.Labels["severity"], "alertname": a.Labels["alertname"],
				"service": service, "namespace": a.Labels["namespace"], "source": "alertmanager",
			}})
		}
		return out, nil
	},
}

// ---------------------------------------------------------------- PagerDuty

var pagerduty = source{
	typ: "pagerduty", name: "PagerDuty (incidents)", provider: "pagerduty", docs: "/docs/connectors/webhooks#pagerduty",
	secretHelp: "Secret de l'abonnement webhook v3 (signature X-PagerDuty-Signature)",
	verify: func(h map[string]string, body []byte, secret string) error {
		want := "v1=" + hmacHex(secret, body)
		for _, sig := range strings.Split(h["x-pagerduty-signature"], ",") {
			if equalSecret(strings.TrimSpace(sig), want) {
				return nil
			}
		}
		return ErrSignature
	},
	parse: func(_ map[string]string, body []byte) ([]connector.Event, error) {
		var p struct {
			Event struct {
				EventType  string `json:"event_type"`
				OccurredAt string `json:"occurred_at"`
				Data       struct {
					ID      string `json:"id"`
					Title   string `json:"title"`
					Urgency string `json:"urgency"`
					Service struct {
						Summary string `json:"summary"`
					} `json:"service"`
				} `json:"data"`
			} `json:"event"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}
		status := ""
		switch p.Event.EventType {
		case "incident.triggered", "incident.reopened":
			status = "open"
		case "incident.resolved":
			status = "resolved"
		default:
			return nil, nil
		}
		d := p.Event.Data
		return []connector.Event{{Kind: model.EventIncident, TS: parseTS(p.Event.OccurredAt), Title: d.Title, Payload: map[string]any{
			"status": status, "incident_key": d.ID, "severity": d.Urgency, "service": d.Service.Summary, "source": "pagerduty",
		}}}, nil
	},
}

// ---------------------------------------------------------------- Opsgenie

var opsgenie = source{
	typ: "opsgenie", name: "Opsgenie (alertes)", provider: "opsgenie", docs: "/docs/connectors/webhooks#opsgenie",
	secretHelp: "Jeton ajouté en en-tête Authorization: Bearer dans l'intégration webhook Opsgenie",
	verify:     verifyBearer,
	parse: func(_ map[string]string, body []byte) ([]connector.Event, error) {
		var p struct {
			Action string `json:"action"`
			Alert  struct {
				AlertID  string   `json:"alertId"`
				Message  string   `json:"message"`
				Priority string   `json:"priority"`
				Entity   string   `json:"entity"`
				Tags     []string `json:"tags"`
			} `json:"alert"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}
		status := ""
		switch p.Action {
		case "Create":
			status = "open"
		case "Close":
			status = "resolved"
		default:
			return nil, nil
		}
		return []connector.Event{{Kind: model.EventIncident, TS: parseTS(""), Title: p.Alert.Message, Payload: map[string]any{
			"status": status, "incident_key": p.Alert.AlertID, "severity": p.Alert.Priority, "service": p.Alert.Entity, "source": "opsgenie",
		}}}, nil
	},
}
