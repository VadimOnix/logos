// logos-server is the Logos control plane: device registry, enrollment,
// agent management channel, and the built-in admin panel. Single binary +
// Postgres by design (PRD §7).
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/VadimOnix/logos/server/internal/api"
	"github.com/VadimOnix/logos/server/internal/auth"
	"github.com/VadimOnix/logos/server/internal/ca"
	"github.com/VadimOnix/logos/server/internal/config"
	"github.com/VadimOnix/logos/server/internal/hub"
	"github.com/VadimOnix/logos/server/internal/store"
)

var version = "0.1.0-dev"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel()}))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func logLevel() slog.Level {
	if strings.EqualFold(os.Getenv("LOGOS_LOG_LEVEL"), "debug") {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	log.Info("logos-server starting", "version", version, "listen", cfg.ListenAddr)

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	if err := bootstrapAdmin(ctx, st, cfg, log); err != nil {
		return err
	}

	authority, err := loadOrCreateCA(ctx, st, log)
	if err != nil {
		return err
	}

	srv := api.NewServer(st, hub.New(), log, authority, cfg.AgentEndpoint, cfg.AgentBinariesDir)
	srv.StartSessionJanitor(ctx)

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverCert, err := authority.ServerCert(cfg.AgentHosts)
	if err != nil {
		return fmt.Errorf("agent listener cert: %w", err)
	}
	agentSrv := &http.Server{
		Addr:    cfg.AgentListen,
		Handler: srv.AgentHandler(),
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{serverCert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    authority.Pool(),
		},
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	go func() { errCh <- agentSrv.ListenAndServeTLS("", "") }()
	log.Info("listening", "api", cfg.ListenAddr, "agents_mtls", cfg.AgentListen, "agent_endpoint", cfg.AgentEndpoint)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		agentSrv.Shutdown(shutdownCtx)
		if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return nil
	}
}

// loadOrCreateCA restores the internal CA from Postgres or mints one on
// first start. A concurrent first start is settled by the database: the
// loser re-reads the winner's CA.
func loadOrCreateCA(ctx context.Context, st *store.Store, log *slog.Logger) (*ca.CA, error) {
	certPEM, keyPEM, err := st.GetCA(ctx)
	if err == nil {
		return ca.Load(certPEM, keyPEM)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	authority, err := ca.Generate()
	if err != nil {
		return nil, err
	}
	certPEM, keyPEM, err = authority.PEM()
	if err != nil {
		return nil, err
	}
	won, err := st.SaveCA(ctx, certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	if !won {
		certPEM, keyPEM, err = st.GetCA(ctx)
		if err != nil {
			return nil, err
		}
		return ca.Load(certPEM, keyPEM)
	}
	log.Info("internal agent CA created")
	return authority, nil
}

// bootstrapAdmin creates the first admin user from LOGOS_ADMIN_EMAIL/PASSWORD
// when the users table is empty, so `docker compose up` yields a usable panel
// with zero manual SQL (PRD F9).
func bootstrapAdmin(ctx context.Context, st *store.Store, cfg *config.Config, log *slog.Logger) error {
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if cfg.AdminEmail == "" || cfg.AdminPassword == "" {
		log.Warn("no users exist and LOGOS_ADMIN_EMAIL/LOGOS_ADMIN_PASSWORD are not set — the panel will be unusable until they are provided")
		return nil
	}
	if len(cfg.AdminPassword) < 10 {
		return fmt.Errorf("LOGOS_ADMIN_PASSWORD must be at least 10 characters")
	}
	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}
	email := strings.ToLower(strings.TrimSpace(cfg.AdminEmail))
	if _, err := st.CreateUser(ctx, email, hash); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	log.Info("bootstrap admin created", "email", email)
	return nil
}
