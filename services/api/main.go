// Commande kairn-api : API publique /api/v1, BFF de l'application web.
//
//	kairn-api            sert l'API (KAIRN_MODE=production|demo)
//	kairn-api migrate    applique les migrations PostgreSQL et ClickHouse
//	kairn-api seed       charge l'organisation de démonstration dans les bases configurées
//
// KAIRN_MODE=demo lance une instance autonome sans dépendance externe :
// stockage en mémoire, bus en processus, workers et ordonnanceur intégrés,
// organisation de démonstration semée au démarrage.
//
// En production sans NATS (KAIRN_NATS_URL vide), l'API exécute aussi les
// workers et l'ordonnanceur (mode nœud unique, petites installations).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/kairn-io/kairn/connectors/all"
	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/config"
	"github.com/kairn-io/kairn/pkg/gateway"
	"github.com/kairn-io/kairn/pkg/jobs"
	"github.com/kairn-io/kairn/pkg/logging"
	"github.com/kairn-io/kairn/pkg/migrate"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/notify"
	"github.com/kairn-io/kairn/pkg/otel"
	"github.com/kairn-io/kairn/pkg/platform"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
	"github.com/kairn-io/kairn/pkg/ratelimit"
	"github.com/kairn-io/kairn/pkg/seed"
	"github.com/kairn-io/kairn/pkg/tenancy"
	"github.com/kairn-io/kairn/services/api/internal/api"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "migrate":
		err = runMigrations()
	case "seed":
		err = runSeed()
	case "import-prices":
		err = runImportPrices(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q (serve, migrate, seed, import-prices)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kairn-api:", err)
		os.Exit(1)
	}
}

// runMigrations applique les migrations avec le rôle propriétaire du schéma
// (KAIRN_POSTGRES_MIGRATE_URL, sinon KAIRN_POSTGRES_URL).
func runMigrations() error {
	cfg, err := config.Load("migrate")
	if err != nil {
		return err
	}
	log := logging.New("migrate", cfg.LogLevel, cfg.LogFormat)
	ctx := context.Background()
	dsn := os.Getenv("KAIRN_POSTGRES_MIGRATE_URL")
	if dsn == "" {
		dsn = cfg.PostgresURL
	}
	if dsn == "" {
		return errors.New("KAIRN_POSTGRES_MIGRATE_URL or KAIRN_POSTGRES_URL is required")
	}
	v, err := migrate.PostgresUp(ctx, dsn)
	if err != nil {
		return err
	}
	log.Info("postgres migrated", "version", v)
	if cfg.ClickHouseAddr != "" {
		cv, err := migrate.ClickHouseUp(ctx, platform.ClickHouse(cfg))
		if err != nil {
			return err
		}
		log.Info("clickhouse migrated", "version", cv)
	}
	return nil
}

// runSeed charge les grilles tarifaires publiques et l'organisation de démonstration.
func runSeed() error {
	cfg, err := config.Load("api")
	if err != nil {
		return err
	}
	log := logging.New("seed", cfg.LogLevel, cfg.LogFormat)
	ctx := context.Background()
	be, err := platform.Open(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer be.Close()
	n, err := catalogs.InstallSamples(ctx, be.Store)
	if err != nil {
		return err
	}
	res, err := seed.Run(ctx, be.Store, be.TSDB, seed.Options{Log: log})
	if err != nil {
		return err
	}
	log.Info("seed done", "catalogs", n, "org", res.OrgID, "created", res.Created, "login", seed.UserEmail)
	return nil
}

func serve() error {
	cfg, err := config.Load("api")
	if err != nil {
		return err
	}
	log := logging.New("api", cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownOTel, err := otel.Setup(ctx, cfg.ServiceName, cfg.OTLPEndpoint)
	if err != nil {
		log.Warn("opentelemetry disabled", "err", err)
	}
	defer shutdownOTel(context.Background())

	var be *platform.Backends
	if cfg.Demo() {
		be, err = platform.Memory(cfg, log)
	} else {
		be, err = platform.Open(ctx, cfg, log)
	}
	if err != nil {
		return err
	}
	defer be.Close()
	if !cfg.Demo() {
		if n, err := catalogs.InstallSamples(ctx, be.Store); err != nil {
			log.Warn("public price catalogs not installed", "err", err)
		} else if n > 0 {
			log.Info("public price catalogs installed", "count", n)
		}
	}

	engine := platform.Engine(cfg, be, log)

	limiter, closeLimiter, err := ratelimit.Open(cfg.RedisURL, api.RateLimitPerSecond, api.RateLimitBurst, log)
	if err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}
	defer closeLimiter()
	deps := api.Deps{
		Store: be.Store, TSDB: be.TSDB, Keyring: be.Keyring, Objects: be.Objects, Bus: be.Bus, Limiter: limiter,
		Jobs:     jobs.BusJobs{Bus: be.Bus},
		Channels: channelTester{engine},
		Sessions: auth.SessionIssuer{Secret: []byte(cfg.SessionSecret), TTL: cfg.SessionTTL},
		Config:   cfg, Log: log, Ready: be.Ready,
	}
	if cfg.OIDCIssuer != "" {
		o, err := api.NewOIDC(ctx, cfg.OIDCIssuer, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.APIURL+api.Prefix+"/auth/oidc/callback")
		if err != nil {
			return err
		}
		deps.OIDC = o
	}
	srv := api.New(deps)
	// En mode nœud unique (démo ou production sans NATS), l'API sert aussi la
	// passerelle d'ingestion (OTLP, webhooks, sondes) et exécute les workers.
	if be.InProcessBus {
		gw := &gateway.Gateway{Store: be.Store, TSDB: be.TSDB, Bus: be.Bus, Keyring: be.Keyring, Log: log, ServiceToken: cfg.ServiceToken}
		srv.Handle("/ingest/", gw.Handler())
		if err := startWorkers(ctx, cfg, log, be); err != nil {
			return err
		}
	}

	httpSrv := &http.Server{Addr: cfg.HTTPAddr, Handler: otel.HTTPHandler(srv, "api"), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 60 * time.Second, WriteTimeout: 10 * time.Minute, IdleTimeout: 120 * time.Second}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr, "mode", cfg.Mode, "single_node", be.InProcessBus)
		if cfg.Demo() {
			log.Info("demo ready", "login", seed.UserEmail, "api", cfg.APIURL, "docs", cfg.APIURL+api.Prefix+"/docs")
		}
		errc <- httpSrv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return httpSrv.Shutdown(sctx)
}

type channelTester struct{ e *notify.Engine }

func (c channelTester) Test(ctx context.Context, id string) error { return c.e.Test(ctx, id) }

// startWorkers démarre workers et ordonnanceur dans le processus ; en démo, sème d'abord l'organisation de démonstration.
func startWorkers(ctx context.Context, cfg config.Config, log *slog.Logger, be *platform.Backends) error {
	if cfg.Demo() && cfg.DemoSeed {
		start := time.Now()
		res, err := seed.Run(ctx, be.Store, be.TSDB, seed.Options{Log: log})
		if err != nil {
			return fmt.Errorf("seed demo: %w", err)
		}
		log.Info("demo organization ready", "org", res.OrgID, "created", res.Created, "duration", time.Since(start).Round(time.Millisecond))
		api.DemoOrgHook = func(ctx context.Context, u model.User) error {
			octx := tenancy.WithUser(tenancy.WithOrg(ctx, seed.OrgID), u.ID)
			if _, err := be.Store.Memberships().Get(octx, u.ID); err == nil {
				return nil
			}
			role := model.RoleAdmin
			if u.Email == seed.UserEmail {
				role = model.RoleOwner
			}
			return be.Store.Memberships().Upsert(octx, &model.Membership{OrgID: seed.OrgID, UserID: u.ID, Role: role})
		}
	}
	worker := platform.Worker(cfg, be, log)
	if err := worker.Subscribe(jobs.RoleIngest, jobs.RoleCost, jobs.RoleNotifier, jobs.RoleAnalytics, jobs.RoleReports); err != nil {
		return err
	}
	sched := &jobs.Scheduler{Worker: worker}
	go func() {
		// Laisse le temps aux services Python de démarrer avant la première analyse.
		select {
		case <-ctx.Done():
			return
		case <-time.After(20 * time.Second):
		}
		sched.Run(ctx)
	}()
	return nil
}
