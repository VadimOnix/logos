// Package config loads control-plane configuration from the environment.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	// ListenAddr is the address the HTTP server binds to (LOGOS_LISTEN).
	ListenAddr string
	// DatabaseURL is the Postgres connection string (LOGOS_DATABASE_URL).
	DatabaseURL string
	// AdminEmail/AdminPassword bootstrap the first admin user when the
	// users table is empty (LOGOS_ADMIN_EMAIL / LOGOS_ADMIN_PASSWORD).
	AdminEmail    string
	AdminPassword string
}

func FromEnv() (*Config, error) {
	cfg := &Config{
		ListenAddr:    envOr("LOGOS_LISTEN", ":8080"),
		DatabaseURL:   os.Getenv("LOGOS_DATABASE_URL"),
		AdminEmail:    os.Getenv("LOGOS_ADMIN_EMAIL"),
		AdminPassword: os.Getenv("LOGOS_ADMIN_PASSWORD"),
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("LOGOS_DATABASE_URL is required")
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
