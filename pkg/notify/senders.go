// Package notify route les alertes vers les canaux (M-08) avec
// déduplication, regroupement, silences et heures ouvrées.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/kairn-io/kairn/pkg/model"
)

// Message est le contenu d'une notification.
type Message struct {
	OrgName     string            `json:"org_name"`
	Kind        string            `json:"kind"`
	Severity    string            `json:"severity"`
	Title       string            `json:"title"`
	Body        string            `json:"body"`
	Link        string            `json:"link"`
	Fingerprint string            `json:"fingerprint"`
	Fields      map[string]string `json:"fields,omitempty"`
	Resolved    bool              `json:"resolved"`
	At          time.Time         `json:"at"`
}

// Sender envoie un message sur un type de canal.
type Sender interface {
	Send(ctx context.Context, ch model.NotificationChannel, secrets map[string]string, msg Message) error
}

// HTTPPoster poste du JSON et vérifie le statut.
type HTTPPoster struct {
	Client *http.Client
}

func (p HTTPPoster) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (p HTTPPoster) post(ctx context.Context, url string, body any, headers map[string]string) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Kairn-Notifier/1.0")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func severityEmoji(sev string, resolved bool) string {
	if resolved {
		return "✅"
	}
	switch sev {
	case "critical":
		return "🔴"
	case "warning":
		return "🟠"
	}
	return "🔵"
}

func prefix(msg Message) string {
	if msg.Resolved {
		return "[Résolu] "
	}
	return ""
}

// Slack envoie via un webhook entrant (blocs).
type Slack struct{ HTTPPoster }

// Send implémente Sender.
func (s Slack) Send(ctx context.Context, ch model.NotificationChannel, sec map[string]string, msg Message) error {
	fields := []map[string]any{}
	for k, v := range msg.Fields {
		fields = append(fields, map[string]any{"type": "mrkdwn", "text": "*" + k + "*\n" + v})
	}
	blocks := []map[string]any{
		{"type": "header", "text": map[string]any{"type": "plain_text", "text": severityEmoji(msg.Severity, msg.Resolved) + " " + prefix(msg) + msg.Title}},
		{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": msg.Body}},
	}
	if len(fields) > 0 {
		blocks = append(blocks, map[string]any{"type": "section", "fields": fields})
	}
	if msg.Link != "" {
		blocks = append(blocks, map[string]any{"type": "actions", "elements": []map[string]any{
			{"type": "button", "text": map[string]any{"type": "plain_text", "text": "Ouvrir dans Kairn"}, "url": msg.Link},
		}})
	}
	body := map[string]any{"text": prefix(msg) + msg.Title, "blocks": blocks}
	if c := ch.Settings["channel"]; c != "" {
		body["channel"] = c
	}
	return s.post(ctx, firstNonEmpty(sec["webhook_url"], sec["url"]), body, nil)
}

// Mattermost envoie via un webhook entrant.
type Mattermost struct{ HTTPPoster }

// Send implémente Sender.
func (m Mattermost) Send(ctx context.Context, ch model.NotificationChannel, sec map[string]string, msg Message) error {
	text := fmt.Sprintf("%s **%s%s**\n%s", severityEmoji(msg.Severity, msg.Resolved), prefix(msg), msg.Title, msg.Body)
	if msg.Link != "" {
		text += "\n[Ouvrir dans Kairn](" + msg.Link + ")"
	}
	body := map[string]any{"text": text, "username": "Kairn"}
	if c := ch.Settings["channel"]; c != "" {
		body["channel"] = c
	}
	return m.post(ctx, firstNonEmpty(sec["webhook_url"], sec["url"]), body, nil)
}

// Teams envoie une carte adaptative via un webhook (Workflows / connecteur entrant).
type Teams struct{ HTTPPoster }

// Send implémente Sender.
func (t Teams) Send(ctx context.Context, _ model.NotificationChannel, sec map[string]string, msg Message) error {
	facts := []map[string]string{}
	for k, v := range msg.Fields {
		facts = append(facts, map[string]string{"title": k, "value": v})
	}
	card := map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json", "type": "AdaptiveCard", "version": "1.4",
		"body": []map[string]any{
			{"type": "TextBlock", "size": "Medium", "weight": "Bolder", "text": prefix(msg) + msg.Title, "wrap": true},
			{"type": "TextBlock", "text": msg.Body, "wrap": true},
			{"type": "FactSet", "facts": facts},
		},
	}
	if msg.Link != "" {
		card["actions"] = []map[string]any{{"type": "Action.OpenUrl", "title": "Ouvrir dans Kairn", "url": msg.Link}}
	}
	body := map[string]any{"type": "message", "attachments": []map[string]any{{"contentType": "application/vnd.microsoft.card.adaptive", "content": card}}}
	return t.post(ctx, firstNonEmpty(sec["webhook_url"], sec["url"]), body, nil)
}

// Webhook poste un JSON signé (HMAC-SHA256) vers une URL cliente.
type Webhook struct{ HTTPPoster }

// Sign calcule la signature « sha256=<hex> » d'un corps.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Send implémente Sender.
func (w Webhook) Send(ctx context.Context, _ model.NotificationChannel, sec map[string]string, msg Message) error {
	raw, _ := json.Marshal(map[string]any{"event": "alert", "alert": msg})
	headers := map[string]string{"X-Kairn-Event": "alert"}
	if s := sec["signing_secret"]; s != "" {
		headers["X-Kairn-Signature"] = Sign(s, raw)
	}
	return w.post(ctx, firstNonEmpty(sec["webhook_url"], sec["url"]), json.RawMessage(raw), headers)
}

// PagerDuty utilise l'API Events v2 (déclenchement et résolution, dédup par empreinte).
type PagerDuty struct {
	HTTPPoster
	URL string // surcharge pour les tests
}

// Send implémente Sender.
func (p PagerDuty) Send(ctx context.Context, _ model.NotificationChannel, sec map[string]string, msg Message) error {
	action := "trigger"
	if msg.Resolved {
		action = "resolve"
	}
	sev := msg.Severity
	switch sev {
	case "critical", "warning", "info", "error":
	default:
		sev = "info"
	}
	url := p.URL
	if url == "" {
		url = "https://events.eu.pagerduty.com/v2/enqueue"
	}
	body := map[string]any{
		"routing_key": sec["routing_key"], "event_action": action, "dedup_key": msg.Fingerprint,
		"payload": map[string]any{"summary": msg.Title, "source": "kairn", "severity": sev, "component": msg.Kind,
			"group": msg.OrgName, "custom_details": msg.Fields},
	}
	if msg.Link != "" {
		body["links"] = []map[string]string{{"href": msg.Link, "text": "Kairn"}}
	}
	return p.post(ctx, url, body, nil)
}

// Email envoie via SMTP (STARTTLS si proposé).
type Email struct {
	Addr, From, User, Pass string
}

// Send implémente Sender.
func (e Email) Send(_ context.Context, ch model.NotificationChannel, _ map[string]string, msg Message) error {
	if e.Addr == "" {
		return fmt.Errorf("smtp is not configured")
	}
	var to []string
	for _, r := range strings.Split(ch.Settings["recipients"], ",") {
		if r = strings.TrimSpace(r); r != "" {
			to = append(to, r)
		}
	}
	if len(to) == 0 {
		return fmt.Errorf("no recipient")
	}
	return SendMail(e, to, prefix(msg)+"[Kairn] "+msg.Title, RenderText(msg), "")
}

// SendMail envoie un e-mail texte (et une pièce jointe PDF optionnelle, encodée par l'appelant).
func SendMail(e Email, to []string, subject, text, rawAttachmentPart string) error {
	host, _, err := net.SplitHostPort(e.Addr)
	if err != nil {
		return fmt.Errorf("smtp addr: %w", err)
	}
	var a smtp.Auth
	if e.User != "" {
		a = smtp.PlainAuth("", e.User, e.Pass, host)
	}
	var b strings.Builder
	b.WriteString("From: " + e.From + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + mimeHeader(subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	if rawAttachmentPart == "" {
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
		b.WriteString(text)
	} else {
		boundary := "kairn-" + fmt.Sprint(time.Now().UnixNano())
		b.WriteString("Content-Type: multipart/mixed; boundary=" + boundary + "\r\n\r\n")
		b.WriteString("--" + boundary + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + text + "\r\n")
		b.WriteString("--" + boundary + "\r\n" + rawAttachmentPart + "\r\n--" + boundary + "--\r\n")
	}
	return smtp.SendMail(e.Addr, a, extractAddr(e.From), to, []byte(b.String()))
}

func mimeHeader(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?UTF-8?B?" + base64Std(s) + "?="
		}
	}
	return s
}

func extractAddr(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.Trim(from[i:], "<>")
	}
	return from
}

// RenderText met en forme une notification en texte brut.
func RenderText(msg Message) string {
	var b strings.Builder
	b.WriteString(prefix(msg) + msg.Title + "\n\n" + msg.Body + "\n")
	for k, v := range msg.Fields {
		b.WriteString("\n" + k + " : " + v)
	}
	if msg.Link != "" {
		b.WriteString("\n\nOuvrir dans Kairn : " + msg.Link + "\n")
	}
	b.WriteString("\n— Kairn (" + msg.OrgName + ")\n")
	return b.String()
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
