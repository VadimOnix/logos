// Package config loads control-plane configuration from the environment.
package config

import (
	"fmt"
	"net"
	"os"
	"strings"
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

	// AgentListen is the dedicated mTLS listener for agent channels
	// (LOGOS_AGENT_LISTEN). The panel/API listener stays plain HTTP behind a
	// reverse proxy; this one terminates TLS itself so client certs work.
	AgentListen string
	// AgentHosts are the names/IPs baked into the agent listener's server
	// cert (LOGOS_AGENT_HOST, comma-separated).
	AgentHosts []string
	// AgentEndpoint is the public URL agents dial, e.g.
	// wss://logos.example.com:8443 (LOGOS_AGENT_ENDPOINT). Derived from
	// AgentHosts + AgentListen when unset.
	AgentEndpoint string
}

func FromEnv() (*Config, error) {
	cfg := &Config{
		ListenAddr:    envOr("LOGOS_LISTEN", ":8080"),
		DatabaseURL:   os.Getenv("LOGOS_DATABASE_URL"),
		AdminEmail:    os.Getenv("LOGOS_ADMIN_EMAIL"),
		AdminPassword: os.Getenv("LOGOS_ADMIN_PASSWORD"),
		AgentListen:   envOr("LOGOS_AGENT_LISTEN", ":8443"),
		AgentEndpoint: os.Getenv("LOGOS_AGENT_ENDPOINT"),
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("LOGOS_DATABASE_URL is required")
	}
	for _, h := range strings.Split(envOr("LOGOS_AGENT_HOST", "localhost,127.0.0.1"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			cfg.AgentHosts = append(cfg.AgentHosts, h)
		}
	}
	if cfg.AgentEndpoint == "" {
		_, port, err := net.SplitHostPort(cfg.AgentListen)
		if err != nil {
			return nil, fmt.Errorf("LOGOS_AGENT_LISTEN: %w", err)
		}
		cfg.AgentEndpoint = "wss://" + net.JoinHostPort(cfg.AgentHosts[0], port)
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
