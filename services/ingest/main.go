// Commande kairn-ingest (M-01) : ingestion distribuée.
//
//	kairn-ingest worker     synchronisations des connecteurs (sujet ingest du bus)
//	kairn-ingest gateway    passerelle HTTP : webhooks, OTLP (agent Kairn), inventaire agent, résultats de sondes
//	kairn-ingest scheduler  ordonnanceur (une seule instance) : publie les tâches périodiques
//	kairn-ingest probe      sondes d'uptime d'une région (KAIRN_REGION), sans accès aux bases
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/kairn-io/kairn/connectors/all"
	"github.com/kairn-io/kairn/pkg/config"
	"github.com/kairn-io/kairn/pkg/gateway"
	"github.com/kairn-io/kairn/pkg/jobs"
	"github.com/kairn-io/kairn/pkg/logging"
	"github.com/kairn-io/kairn/pkg/otel"
	"github.com/kairn-io/kairn/pkg/platform"
	"github.com/kairn-io/kairn/pkg/probe"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: kairn-ingest worker|gateway|scheduler|probe")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "kairn-ingest:", err)
		os.Exit(1)
	}
}

func run(role string) error {
	service := "ingest"
	if role == "probe" {
		service = "probe"
	}
	cfg, err := config.Load(service)
	if err != nil {
		return err
	}
	log := logging.New("ingest-"+role, cfg.LogLevel, cfg.LogFormat)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownOTel, err := otel.Setup(ctx, cfg.ServiceName+"-"+role, cfg.OTLPEndpoint)
	if err != nil {
		log.Warn("opentelemetry disabled", "err", err)
	}
	defer shutdownOTel(context.Background())

	if role == "probe" {
		gw := os.Getenv("KAIRN_GATEWAY_URL")
		if gw == "" || cfg.ServiceToken == "" {
			return errors.New("KAIRN_GATEWAY_URL and KAIRN_SERVICE_TOKEN are required")
		}
		r := &probe.Runner{GatewayURL: gw, ServiceToken: cfg.ServiceToken, Region: cfg.Region, Log: log,
			AllowPrivate: os.Getenv("KAIRN_PROBE_ALLOW_PRIVATE") == "true"}
		go r.Run(ctx)
		return platform.Serve(ctx, platform.HealthServer(cfg.HTTPAddr, nil, nil), log)
	}

	be, err := platform.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer be.Close()
	if be.InProcessBus {
		return errors.New("KAIRN_NATS_URL is required for distributed ingestion (single-node deployments run everything in kairn-api)")
	}
	switch role {
	case "worker":
		if err := platform.Worker(cfg, be, log).Subscribe(jobs.RoleIngest); err != nil {
			return err
		}
		return platform.Serve(ctx, platform.HealthServer(cfg.HTTPAddr, be.Ready, nil), log)
	case "gateway":
		gw := &gateway.Gateway{Store: be.Store, TSDB: be.TSDB, Bus: be.Bus, Keyring: be.Keyring, Log: log, ServiceToken: cfg.ServiceToken}
		return platform.Serve(ctx, platform.HealthServer(cfg.HTTPAddr, be.Ready, otel.HTTPHandler(gw.Handler(), "ingest-gateway")), log)
	case "scheduler":
		sched := &jobs.Scheduler{Worker: platform.Worker(cfg, be, log)}
		go sched.Run(ctx)
		return platform.Serve(ctx, platform.HealthServer(cfg.HTTPAddr, be.Ready, nil), log)
	}
	return fmt.Errorf("unknown role %q", role)
}
