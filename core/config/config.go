// Package config loads daemon configuration from the environment.
// Only non-secret settings live here; credentials are always in the
// system keyring (internal/keyring) and per-account settings in the
// Account.config JSON column.
package config

import "github.com/caarlos0/env/v11"

type Config struct {
	// APIAddr is the localhost-only HTTP API listen address. Port 0 picks
	// an ephemeral port that is handed to the QML UI via environment.
	APIAddr string `env:"DMAIL_API_ADDR" envDefault:"127.0.0.1:0"`
	// OAuthBindAddr is the loopback listener for the OAuth redirect.
	OAuthBindAddr string `env:"DMAIL_OAUTH_ADDR" envDefault:"127.0.0.1:0"`
	// DatabasePath overrides the default XDG data location of the SQLite DB.
	DatabasePath string `env:"DMAIL_DB_PATH"`
	// Google OAuth client. The user supplies their own client ID/secret
	// (see docs/gmail-setup.md); these are NOT secrets in the OAuth
	// desktop-app model, but tokens obtained with them are (→ keyring).
	GoogleClientID     string `env:"DMAIL_GOOGLE_CLIENT_ID"`
	GoogleClientSecret string `env:"DMAIL_GOOGLE_CLIENT_SECRET"`
	DisableHTTP        bool   `env:"DMAIL_DISABLE_HTTP"`
	DisableIPC         bool   `env:"DMAIL_DISABLE_IPC"`
	LogLevel           string `env:"DMAIL_LOG_LEVEL" envDefault:"info"`
	// BodyCapKB caps stored plain-text bodies per message.
	BodyCapKB int `env:"DMAIL_BODY_CAP_KB" envDefault:"32"`
	// RetentionDays: threads older than this are pruned unless starred or
	// snoozed.
	RetentionDays int `env:"DMAIL_RETENTION_DAYS" envDefault:"30"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
