package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/bionicpro/reports-service/internal/auth"
	"github.com/bionicpro/reports-service/internal/config"
	"github.com/bionicpro/reports-service/internal/reports"
	"github.com/bionicpro/reports-service/internal/server"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("reports-service: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := reports.Connect(ctx, cfg.ClickHouseAddr, cfg.ClickHouseDB, cfg.ClickHouseUser, cfg.ClickHousePassword)
	if err != nil {
		return err
	}
	defer store.Close()

	verifier, err := waitForIssuer(ctx, cfg)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.New(cfg, verifier, store).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s, issuer %s", cfg.ListenAddr, cfg.PublicIssuer())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func waitForIssuer(ctx context.Context, cfg *config.Config) (*auth.Verifier, error) {
	var lastErr error

	for attempt := 1; attempt <= 30; attempt++ {
		verifier, err := auth.NewVerifier(ctx, cfg.InternalIssuer(), cfg.PublicIssuer(), cfg.RequiredRole)
		if err == nil {
			return verifier, nil
		}
		lastErr = err
		log.Printf("waiting for keycloak (%d/30): %v", attempt, err)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return nil, lastErr
}
