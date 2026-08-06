package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr string

	KeycloakInternalURL string
	KeycloakPublicURL   string
	Realm               string
	RequiredRole        string

	ClickHouseAddr     string
	ClickHouseDB       string
	ClickHouseUser     string
	ClickHousePassword string

	S3Endpoint      string
	S3AccessKey     string
	S3SecretKey     string
	S3Bucket        string
	S3UseSSL        bool
	S3RetentionDays int

	CDNBaseURL string
	CDNSecret  string
	CDNLinkTTL time.Duration
}

func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:          env("LISTEN_ADDR", ":8001"),
		KeycloakInternalURL: strings.TrimRight(env("KEYCLOAK_INTERNAL_URL", "http://keycloak:8080"), "/"),
		KeycloakPublicURL:   strings.TrimRight(env("KEYCLOAK_PUBLIC_URL", "http://localhost:8080"), "/"),
		Realm:               env("KEYCLOAK_REALM", "reports-realm"),
		RequiredRole:        env("REQUIRED_ROLE", "prothetic_user"),
		ClickHouseAddr:      env("CLICKHOUSE_ADDR", "clickhouse:9000"),
		ClickHouseDB:        env("CLICKHOUSE_DB", "bionicpro"),
		ClickHouseUser:      env("CLICKHOUSE_USER", "default"),
		ClickHousePassword:  os.Getenv("CLICKHOUSE_PASSWORD"),
		S3Endpoint:          env("S3_ENDPOINT", "minio:9000"),
		S3AccessKey:         env("S3_ACCESS_KEY", "minio_user"),
		S3SecretKey:         env("S3_SECRET_KEY", "minio_password"),
		S3Bucket:            env("S3_BUCKET", "reports"),
		CDNBaseURL:          strings.TrimRight(env("CDN_BASE_URL", "http://localhost:8090"), "/"),
		CDNSecret:           os.Getenv("CDN_SECRET"),
	}

	if cfg.Realm == "" {
		return nil, fmt.Errorf("KEYCLOAK_REALM is required")
	}
	if cfg.CDNSecret == "" {
		return nil, fmt.Errorf("CDN_SECRET is required")
	}

	var err error
	if cfg.S3UseSSL, err = boolean("S3_USE_SSL", false); err != nil {
		return nil, err
	}
	if cfg.S3RetentionDays, err = integer("S3_RETENTION_DAYS", 30); err != nil {
		return nil, err
	}
	if cfg.CDNLinkTTL, err = duration("CDN_LINK_TTL", 15*time.Minute); err != nil {
		return nil, err
	}
	return cfg, nil
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

func integer(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return v, nil
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

func (c *Config) InternalIssuer() string {
	return fmt.Sprintf("%s/realms/%s", c.KeycloakInternalURL, c.Realm)
}

func (c *Config) PublicIssuer() string {
	return fmt.Sprintf("%s/realms/%s", c.KeycloakPublicURL, c.Realm)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
