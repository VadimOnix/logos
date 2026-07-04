// Package config loads control-plane configuration from the environment.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
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
	// AgentBinariesDir holds cross-compiled agent binaries named
	// logos-agent-linux-<goarch> for the adoption tool to download
	// (LOGOS_AGENT_BINARIES_DIR); empty disables the endpoint.
	AgentBinariesDir string

	// Node-offline alerting (F11). Alerts are enabled when a webhook URL,
	// Telegram bot, and/or SMTP settings are present.
	AlertWebhookURL    string        // LOGOS_ALERT_WEBHOOK_URL
	AlertOfflineAfter  time.Duration // LOGOS_ALERT_OFFLINE_AFTER (default 3m)
	AlertDiskPct       float64       // LOGOS_ALERT_DISK_PCT (default 90; 0 disables)
	AlertTelegramToken string        // LOGOS_ALERT_TELEGRAM_TOKEN (bot token)
	AlertTelegramChat  string        // LOGOS_ALERT_TELEGRAM_CHAT (chat id or @channel)
	SMTPAddr           string        // LOGOS_SMTP_ADDR (host:port)
	SMTPFrom           string        // LOGOS_SMTP_FROM
	SMTPTo             []string      // LOGOS_SMTP_TO (comma-separated)
	SMTPUser           string        // LOGOS_SMTP_USER (optional)
	SMTPPassword       string        // LOGOS_SMTP_PASSWORD (optional)
}

func FromEnv() (*Config, error) {
	cfg := &Config{
		ListenAddr:       envOr("LOGOS_LISTEN", ":8080"),
		DatabaseURL:      os.Getenv("LOGOS_DATABASE_URL"),
		AdminEmail:       os.Getenv("LOGOS_ADMIN_EMAIL"),
		AdminPassword:    os.Getenv("LOGOS_ADMIN_PASSWORD"),
		AgentListen:      envOr("LOGOS_AGENT_LISTEN", ":8443"),
		AgentEndpoint:    os.Getenv("LOGOS_AGENT_ENDPOINT"),
		AgentBinariesDir: os.Getenv("LOGOS_AGENT_BINARIES_DIR"),
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

	cfg.AlertWebhookURL = os.Getenv("LOGOS_ALERT_WEBHOOK_URL")
	cfg.SMTPAddr = os.Getenv("LOGOS_SMTP_ADDR")
	cfg.SMTPFrom = os.Getenv("LOGOS_SMTP_FROM")
	cfg.SMTPUser = os.Getenv("LOGOS_SMTP_USER")
	cfg.SMTPPassword = os.Getenv("LOGOS_SMTP_PASSWORD")
	for _, to := range strings.Split(os.Getenv("LOGOS_SMTP_TO"), ",") {
		if to = strings.TrimSpace(to); to != "" {
			cfg.SMTPTo = append(cfg.SMTPTo, to)
		}
	}
	if cfg.SMTPAddr != "" && (cfg.SMTPFrom == "" || len(cfg.SMTPTo) == 0) {
		return nil, fmt.Errorf("LOGOS_SMTP_ADDR is set but LOGOS_SMTP_FROM/LOGOS_SMTP_TO are missing")
	}
	cfg.AlertTelegramToken = os.Getenv("LOGOS_ALERT_TELEGRAM_TOKEN")
	cfg.AlertTelegramChat = os.Getenv("LOGOS_ALERT_TELEGRAM_CHAT")
	if (cfg.AlertTelegramToken == "") != (cfg.AlertTelegramChat == "") {
		return nil, fmt.Errorf("LOGOS_ALERT_TELEGRAM_TOKEN and LOGOS_ALERT_TELEGRAM_CHAT must be set together")
	}
	cfg.AlertOfflineAfter = 3 * time.Minute
	if v := os.Getenv("LOGOS_ALERT_OFFLINE_AFTER"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 30*time.Second {
			return nil, fmt.Errorf("LOGOS_ALERT_OFFLINE_AFTER must be a duration >= 30s (e.g. 3m)")
		}
		cfg.AlertOfflineAfter = d
	}
	cfg.AlertDiskPct = 90
	if v := os.Getenv("LOGOS_ALERT_DISK_PCT"); v != "" {
		p, err := strconv.ParseFloat(v, 64)
		if err != nil || p < 0 || p >= 100 {
			return nil, fmt.Errorf("LOGOS_ALERT_DISK_PCT must be a number in [0,100) (e.g. 90; 0 disables)")
		}
		cfg.AlertDiskPct = p
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
