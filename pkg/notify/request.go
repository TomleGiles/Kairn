package notify

// AlertRequest est une demande d'alerte publiée sur le bus (sujet
// kairn.events.alert) par l'API, l'analytics, l'ingestion ou les sondes.
// Le notifier la rapproche des règles d'alerte de l'organisation.
type AlertRequest struct {
	Kind        string         `json:"kind"`
	Severity    string         `json:"severity"`
	Fingerprint string         `json:"fingerprint"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	Link        string         `json:"link"`
	Payload     map[string]any `json:"payload,omitempty"`
	// RuleID force une règle précise (ex. alerte de budget liée à ses canaux).
	RuleID string `json:"rule_id,omitempty"`
	// ChannelIDs envoie directement vers des canaux (budgets), en plus des règles.
	ChannelIDs []string `json:"channel_ids,omitempty"`
	// Resolved signale la fin de la condition (résolution de l'alerte).
	Resolved bool `json:"resolved,omitempty"`
}
