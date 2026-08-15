package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/bionicpro/bionicpro-auth/internal/config"
	"github.com/bionicpro/bionicpro-auth/internal/oidc"
	"github.com/bionicpro/bionicpro-auth/internal/secure"
	"github.com/bionicpro/bionicpro-auth/internal/server"
	"github.com/bionicpro/bionicpro-auth/internal/session"
)

const maxAccessTokenLifespan = 2 * time.Minute

func main() {
	if err := run(); err != nil {
		log.Fatalf("bionicpro-auth: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.SessionTTL <= maxAccessTokenLifespan {
		return errors.New("SESSION_TTL must be longer than the access token lifespan (2m)")
	}

	cipher, err := secure.NewCipher(cfg.EncryptionKey)
	if err != nil {
		return err
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword})
	defer rdb.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		return err
	}

	handler, err := server.New(cfg,
		oidc.New(cfg.KeycloakPublicURL, cfg.KeycloakURL, cfg.Realm, cfg.ClientID, cfg.ClientSecret),
		session.NewStore(rdb, cipher, cfg.SessionTTL))
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s, realm %s, session ttl %s", cfg.ListenAddr, cfg.Realm, cfg.SessionTTL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	return srv.Shutdown(shutdownCtx)
}
