package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr string

	KeycloakURL       string
	KeycloakPublicURL string
	Realm             string
	ClientID          string
	ClientSecret      string
	RedirectURI       string

	FrontendURL   string
	ReportsAPIURL string

	RedisAddr     string
	RedisPassword string

	SessionTTL   time.Duration
	LoginTTL     time.Duration
	CookieName   string
	CookieSecure bool

	EncryptionKey []byte
}

func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:        env("LISTEN_ADDR", ":8000"),
		KeycloakURL:       strings.TrimRight(env("KEYCLOAK_URL", "http://localhost:8080"), "/"),
		Realm:             env("KEYCLOAK_REALM", "reports-realm"),
		KeycloakPublicURL: strings.TrimRight(env("KEYCLOAK_PUBLIC_URL", env("KEYCLOAK_URL", "http://localhost:8080")), "/"),
		ClientID:          env("KEYCLOAK_CLIENT_ID", "bionicpro-auth"),
		ClientSecret:      os.Getenv("KEYCLOAK_CLIENT_SECRET"),
		RedirectURI:       env("REDIRECT_URI", "http://localhost:8000/auth/callback"),
		FrontendURL:       strings.TrimRight(env("FRONTEND_URL", "http://localhost:3000"), "/"),
		ReportsAPIURL:     strings.TrimRight(os.Getenv("REPORTS_API_URL"), "/"),
		RedisAddr:         env("REDIS_ADDR", "localhost:6379"),
		RedisPassword:     os.Getenv("REDIS_PASSWORD"),
		CookieName:        env("SESSION_COOKIE_NAME", "bp_session"),
	}

	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("KEYCLOAK_CLIENT_SECRET is required")
	}

	var err error
	if cfg.SessionTTL, err = duration("SESSION_TTL", 30*time.Minute); err != nil {
		return nil, err
	}
	if cfg.LoginTTL, err = duration("LOGIN_STATE_TTL", 5*time.Minute); err != nil {
		return nil, err
	}
	if cfg.CookieSecure, err = boolean("COOKIE_SECURE", true); err != nil {
		return nil, err
	}

	key := os.Getenv("SESSION_ENC_KEY")
	if key == "" {
		return nil, fmt.Errorf("SESSION_ENC_KEY is required (base64 of 32 random bytes)")
	}
	if cfg.EncryptionKey, err = base64.StdEncoding.DecodeString(key); err != nil {
		return nil, fmt.Errorf("SESSION_ENC_KEY is not valid base64: %w", err)
	}
	if len(cfg.EncryptionKey) != 32 {
		return nil, fmt.Errorf("SESSION_ENC_KEY must decode to 32 bytes, got %d", len(cfg.EncryptionKey))
	}

	return cfg, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return v, nil
}

func boolean(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return v, nil
}
