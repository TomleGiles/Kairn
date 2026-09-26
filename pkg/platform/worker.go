package platform

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/kairn-io/kairn/pkg/config"
	"github.com/kairn-io/kairn/pkg/costrun"
	"github.com/kairn-io/kairn/pkg/ingest"
	"github.com/kairn-io/kairn/pkg/jobs"
	"github.com/kairn-io/kairn/pkg/notify"
)

// Email renvoie la configuration SMTP.
func Email(cfg config.Config) notify.Email {
	return notify.Email{Addr: cfg.SMTPAddr, From: cfg.SMTPFrom, User: cfg.SMTPUser, Pass: cfg.SMTPPass}
}

// Engine construit le moteur de notifications (M-08).
func Engine(cfg config.Config, be *Backends, log *slog.Logger) *notify.Engine {
	return &notify.Engine{Store: be.Store, TSDB: be.TSDB, Keyring: be.Keyring, PublicURL: cfg.PublicURL, Log: log,
		Senders: notify.DefaultSenders(Email(cfg), notify.HTTPPoster{})}
}

// Worker construit le worker de tâches ; Subscribe choisit ensuite les rôles servis par le processus.
func Worker(cfg config.Config, be *Backends, log *slog.Logger) *jobs.Worker {
	return &jobs.Worker{
		Store: be.Store, TSDB: be.TSDB, Bus: be.Bus, Keyring: be.Keyring, Objects: be.Objects, Log: log, Notify: Engine(cfg, be, log),
		Syncer:       &ingest.Syncer{Store: be.Store, TSDB: be.TSDB, Keyring: be.Keyring, Log: log},
		Costs:        &costrun.Runner{Store: be.Store, TSDB: be.TSDB, Log: log},
		AnalyticsURL: cfg.AnalyticsServiceURL, AIURL: cfg.AIServiceURL, ServiceToken: cfg.ServiceToken,
		Email: Email(cfg), PriceImport: cfg.PriceImport,
	}
}

// HealthServer sert /healthz (vivant) et /readyz (dépendances disponibles) pour les sondes Kubernetes.
func HealthServer(addr string, ready func(context.Context) error, extra http.Handler) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			if err := ready(r.Context()); err != nil {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
		}
		_, _ = w.Write([]byte("ready"))
	})
	if extra != nil {
		mux.Handle("/", extra)
	}
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second}
}

// Serve démarre srv et l'arrête proprement à l'annulation de ctx.
func Serve(ctx context.Context, srv *http.Server, log *slog.Logger) error {
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		errc <- srv.ListenAndServe()
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
	return srv.Shutdown(sctx)
}
