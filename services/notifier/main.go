// Commande kairn-notifier (M-08, M-10) : évaluation des règles d'alerte,
// regroupement, silences, heures ouvrées et envoi sur les canaux (e-mail,
// Slack, Teams, Mattermost, webhook, PagerDuty) ; génération et envoi des
// rapports mensuels via ai-service.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kairn-io/kairn/pkg/config"
	"github.com/kairn-io/kairn/pkg/jobs"
	"github.com/kairn-io/kairn/pkg/logging"
	"github.com/kairn-io/kairn/pkg/otel"
	"github.com/kairn-io/kairn/pkg/platform"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "kairn-notifier:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("notifier")
	if err != nil {
		return err
	}
	log := logging.New("notifier", cfg.LogLevel, cfg.LogFormat)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownOTel, err := otel.Setup(ctx, cfg.ServiceName, cfg.OTLPEndpoint)
	if err != nil {
		log.Warn("opentelemetry disabled", "err", err)
	}
	defer shutdownOTel(context.Background())

	be, err := platform.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer be.Close()
	if be.InProcessBus {
		return errors.New("KAIRN_NATS_URL is required (single-node deployments run everything in kairn-api)")
	}
	if err := platform.Worker(cfg, be, log).Subscribe(jobs.RoleNotifier, jobs.RoleReports); err != nil {
		return err
	}
	return platform.Serve(ctx, platform.HealthServer(cfg.HTTPAddr, be.Ready, nil), log)
}
