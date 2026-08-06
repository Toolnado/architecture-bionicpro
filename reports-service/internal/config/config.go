package config

import (
	"fmt"
	"os"
	"strings"
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
	}

	if cfg.Realm == "" {
		return nil, fmt.Errorf("KEYCLOAK_REALM is required")
	}
	return cfg, nil
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
