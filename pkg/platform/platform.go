// Package platform ouvre les backends de production d'un service Kairn à partir
// de la configuration : PostgreSQL (pgstore), ClickHouse (chtsdb), NATS JetStream
// (ou bus en processus en mode tout-en-un), stockage objet S3 (ou disque local),
// chiffrement des secrets (Vault Transit ou KEK locale).
package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kairn-io/kairn/pkg/bus"
	"github.com/kairn-io/kairn/pkg/config"
	"github.com/kairn-io/kairn/pkg/objstore"
	"github.com/kairn-io/kairn/pkg/secrets"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/store/memstore"
	"github.com/kairn-io/kairn/pkg/store/pgstore"
	"github.com/kairn-io/kairn/pkg/tsdb"
	"github.com/kairn-io/kairn/pkg/tsdb/chtsdb"
	"github.com/kairn-io/kairn/pkg/tsdb/memtsdb"
)

// Backends regroupe les dépendances d'un service.
type Backends struct {
	Store   store.Store
	TSDB    tsdb.TSDB
	Bus     bus.Bus
	Objects objstore.Store
	Keyring *secrets.Keyring
	// InProcessBus : pas de NATS, les workers doivent tourner dans le même processus (mode tout-en-un).
	InProcessBus bool
	// Ready vérifie la disponibilité des dépendances (sonde /readyz).
	Ready   func(context.Context) error
	closers []func()
}

// Close libère les ressources dans l'ordre inverse d'ouverture.
func (b *Backends) Close() {
	for i := len(b.closers) - 1; i >= 0; i-- {
		b.closers[i]()
	}
}

// Keyring construit le trousseau de chiffrement : Vault Transit si configuré, sinon KEK locale.
func Keyring(cfg config.Config) (*secrets.Keyring, error) {
	if cfg.VaultAddr != "" {
		return secrets.NewKeyring(&secrets.VaultTransit{Addr: cfg.VaultAddr, Token: cfg.VaultToken, KeyName: cfg.VaultKey,
			Client: &http.Client{Timeout: 10 * time.Second}}), nil
	}
	if cfg.KEK == "" {
		return nil, errors.New("platform: KAIRN_KEK or KAIRN_VAULT_ADDR is required")
	}
	w, err := secrets.NewLocalKEK(cfg.KEK)
	if err != nil {
		return nil, err
	}
	return secrets.NewKeyring(w), nil
}

// ClickHouse construit le client HTTP ClickHouse.
func ClickHouse(cfg config.Config) *chtsdb.Client {
	return &chtsdb.Client{URL: cfg.ClickHouseAddr, Database: cfg.ClickHouseDB, User: cfg.ClickHouseUser, Password: cfg.ClickHousePassword}
}

// Open ouvre les backends de production.
func Open(ctx context.Context, cfg config.Config, log *slog.Logger) (*Backends, error) {
	b := &Backends{}
	fail := func(err error) (*Backends, error) {
		b.Close()
		return nil, err
	}
	kr, err := Keyring(cfg)
	if err != nil {
		return fail(err)
	}
	b.Keyring = kr

	pg, err := pgstore.Open(ctx, cfg.PostgresURL)
	if err != nil {
		return fail(err)
	}
	b.Store = pg
	b.closers = append(b.closers, pg.Close)

	ch := ClickHouse(cfg)
	b.TSDB = chtsdb.New(ch)

	if cfg.NATSURL != "" {
		nb, err := bus.ConnectNATS(ctx, cfg.NATSURL, cfg.ServiceName, log)
		if err != nil {
			return fail(fmt.Errorf("platform: nats: %w", err))
		}
		b.Bus = nb
		b.closers = append(b.closers, func() { _ = nb.Close() })
	} else {
		mb := bus.NewMemory(log)
		b.Bus, b.InProcessBus = mb, true
		b.closers = append(b.closers, func() { _ = mb.Close() })
		log.Warn("KAIRN_NATS_URL not set: in-process bus, workers run inside this process (single-node mode)")
	}

	if cfg.S3Endpoint != "" {
		s3, err := objstore.NewS3(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3Region, cfg.S3UseSSL)
		if err != nil {
			return fail(err)
		}
		if err := s3.EnsureBucket(ctx, cfg.S3Region); err != nil {
			return fail(fmt.Errorf("platform: s3 bucket: %w", err))
		}
		b.Objects = s3
	} else {
		b.Objects = objstore.Local{Dir: cfg.LocalObjectDir}
		log.Warn("KAIRN_S3_ENDPOINT not set: reports and exports are stored on local disk", "dir", cfg.LocalObjectDir)
	}

	b.Ready = func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := pg.Ping(ctx); err != nil {
			return fmt.Errorf("postgres: %w", err)
		}
		if err := ch.Ping(ctx); err != nil {
			return fmt.Errorf("clickhouse: %w", err)
		}
		return nil
	}
	return b, nil
}

// Memory ouvre des backends entièrement en mémoire (mode démo, tests).
func Memory(cfg config.Config, log *slog.Logger) (*Backends, error) {
	kek := cfg.KEK
	if kek == "" {
		var err error
		if kek, err = secrets.GenerateKEK(); err != nil {
			return nil, err
		}
	}
	w, err := secrets.NewLocalKEK(kek)
	if err != nil {
		return nil, err
	}
	mb := bus.NewMemory(log)
	return &Backends{
		Store: memstore.New(), TSDB: memtsdb.New(), Bus: mb, InProcessBus: true,
		Objects: objstore.Local{Dir: cfg.LocalObjectDir}, Keyring: secrets.NewKeyring(w),
		Ready:   func(context.Context) error { return nil },
		closers: []func(){func() { _ = mb.Close() }},
	}, nil
}
