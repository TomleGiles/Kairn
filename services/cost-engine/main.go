// Commande kairn-cost-engine (M-03, M-04) : calcul déterministe et rejouable
// des coûts (tarification, allocation, coûts partagés) sur demande du bus,
// puis déclenchement de l'analyse (anomalies, recommandations, prévisions).
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
		fmt.Fprintln(os.Stderr, "kairn-cost-engine:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("cost-engine")
	if err != nil {
		return err
	}
	log := logging.New("cost-engine", cfg.LogLevel, cfg.LogFormat)
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
	if err := platform.Worker(cfg, be, log).Subscribe(jobs.RoleCost, jobs.RoleAnalytics); err != nil {
		return err
	}
	return platform.Serve(ctx, platform.HealthServer(cfg.HTTPAddr, be.Ready, nil), log)
}
