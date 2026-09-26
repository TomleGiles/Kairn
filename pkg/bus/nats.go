package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// StreamName est le flux JetStream de Kairn (rejouable).
const StreamName = "KAIRN"

// NATS implémente Bus sur JetStream.
type NATS struct {
	nc   *nats.Conn
	js   jetstream.JetStream
	log  *slog.Logger
	ctxs []jetstream.ConsumeContext
}

// ConnectNATS se connecte et s'assure de l'existence du flux.
func ConnectNATS(ctx context.Context, url, name string, log *slog.Logger) (*NATS, error) {
	if log == nil {
		log = slog.Default()
	}
	nc, err := nats.Connect(url, nats.Name(name), nats.MaxReconnects(-1), nats.ReconnectWait(2*time.Second))
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     StreamName,
		Subjects: []string{"kairn.>"},
		// Rétention par limites : chaque consommateur durable reçoit tous les messages de son sujet.
		Retention: jetstream.LimitsPolicy,
		MaxAge:    7 * 24 * time.Hour,
		Storage:   jetstream.FileStorage,
		Replicas:  1,
	})
	if err != nil && !strings.Contains(err.Error(), "already") {
		nc.Close()
		return nil, fmt.Errorf("create stream: %w", err)
	}
	return &NATS{nc: nc, js: js, log: log}, nil
}

// Publish publie un message persistant.
func (n *NATS) Publish(ctx context.Context, subject, orgID string, data any) error {
	msg, err := NewMessage(subject, orgID, data)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = n.js.Publish(ctx, subject, raw)
	return err
}

// Subscribe crée (ou reprend) un consommateur durable partagé par le groupe.
func (n *NATS) Subscribe(subject, group string, h Handler) error {
	ctx := context.Background()
	durable := strings.NewReplacer(".", "_", ">", "all", "*", "any").Replace(group + "_" + subject)
	cons, err := n.js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:       durable,
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       5 * time.Minute,
		MaxDeliver:    5,
		BackOff:       []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute},
	})
	if err != nil {
		return fmt.Errorf("create consumer %s: %w", durable, err)
	}
	cc, err := cons.Consume(func(m jetstream.Msg) {
		var msg Message
		if err := json.Unmarshal(m.Data(), &msg); err != nil {
			n.log.Error("bus: invalid message, dropped", "subject", m.Subject(), "err", err)
			_ = m.Term()
			return
		}
		if err := h(context.Background(), msg); err != nil {
			n.log.Warn("bus handler failed, will retry", "subject", subject, "org", msg.OrgID, "err", err)
			_ = m.Nak()
			return
		}
		_ = m.Ack()
	})
	if err != nil {
		return fmt.Errorf("consume %s: %w", durable, err)
	}
	n.ctxs = append(n.ctxs, cc)
	return nil
}

// Close arrête les consommateurs et la connexion.
func (n *NATS) Close() error {
	for _, c := range n.ctxs {
		c.Stop()
	}
	if n.nc == nil {
		return errors.New("bus: not connected")
	}
	return n.nc.Drain()
}
