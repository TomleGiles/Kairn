// Package bus abstrait la messagerie inter-services (NATS JetStream en
// production, en mémoire pour le mode démo et les tests). Les messages sont
// JSON et portent toujours l'organisation concernée.
package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Sujets publiés sur le bus.
const (
	SubjectSyncRequested   = "kairn.jobs.sync"          // demande de synchronisation d'un connecteur
	SubjectConnectorSynced = "kairn.events.synced"      // synchronisation terminée
	SubjectCostRequested   = "kairn.jobs.cost"          // recalcul de coûts
	SubjectCostComputed    = "kairn.events.cost"        // coûts recalculés
	SubjectAnalytics       = "kairn.jobs.analytics"     // anomalies, recommandations, prévisions
	SubjectAlert           = "kairn.events.alert"       // alerte à notifier
	SubjectReport          = "kairn.jobs.report"        // génération de rapport
	SubjectUptimeResult    = "kairn.events.uptime"      // résultat de sonde
	SubjectWebhookOut      = "kairn.events.webhook_out" // événement à pousser vers les webhooks clients
)

// Message est l'enveloppe commune.
type Message struct {
	OrgID   string          `json:"org_id"`
	Subject string          `json:"subject"`
	At      time.Time       `json:"at"`
	Data    json.RawMessage `json:"data"`
}

// Decode décode la charge utile.
func (m Message) Decode(v any) error { return json.Unmarshal(m.Data, v) }

// Handler traite un message ; une erreur provoque une nouvelle livraison (NATS).
type Handler func(ctx context.Context, m Message) error

// Bus publie et consomme des messages.
type Bus interface {
	Publish(ctx context.Context, subject, orgID string, data any) error
	// Subscribe consomme un sujet dans un groupe (un seul consommateur du groupe reçoit chaque message).
	Subscribe(subject, group string, h Handler) error
	Close() error
}

// NewMessage construit une enveloppe.
func NewMessage(subject, orgID string, data any) (Message, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return Message{}, fmt.Errorf("bus: encode: %w", err)
	}
	return Message{OrgID: orgID, Subject: subject, At: time.Now().UTC(), Data: raw}, nil
}

// Memory est un bus en mémoire, asynchrone, pour un seul processus.
type Memory struct {
	mu     sync.RWMutex
	groups map[string]map[string]Handler // sujet → groupe → handler
	wg     sync.WaitGroup
	log    *slog.Logger
	closed bool
	// Sync force l'exécution synchrone (tests).
	Sync bool
}

// NewMemory crée un bus mémoire.
func NewMemory(log *slog.Logger) *Memory {
	if log == nil {
		log = slog.Default()
	}
	return &Memory{groups: map[string]map[string]Handler{}, log: log}
}

// Publish distribue le message à un handler de chaque groupe abonné.
func (m *Memory) Publish(ctx context.Context, subject, orgID string, data any) error {
	msg, err := NewMessage(subject, orgID, data)
	if err != nil {
		return err
	}
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return fmt.Errorf("bus: closed")
	}
	var hs []Handler
	for _, h := range m.groups[subject] {
		hs = append(hs, h)
	}
	m.mu.RUnlock()
	for _, h := range hs {
		if m.Sync {
			if err := h(context.WithoutCancel(ctx), msg); err != nil {
				m.log.Warn("bus handler failed", "subject", subject, "org", orgID, "err", err)
			}
			continue
		}
		m.wg.Add(1)
		go func(h Handler) {
			defer m.wg.Done()
			if err := h(context.WithoutCancel(ctx), msg); err != nil {
				m.log.Warn("bus handler failed", "subject", subject, "org", orgID, "err", err)
			}
		}(h)
	}
	return nil
}

// Subscribe enregistre un handler pour un groupe.
func (m *Memory) Subscribe(subject, group string, h Handler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.groups[subject] == nil {
		m.groups[subject] = map[string]Handler{}
	}
	m.groups[subject][group] = h
	return nil
}

// Wait attend la fin des traitements en cours (tests).
func (m *Memory) Wait() { m.wg.Wait() }

// Close arrête le bus après les traitements en cours.
func (m *Memory) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}
