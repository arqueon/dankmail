package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	gosync "sync"

	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/config"
	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/internal/accounts"
	"github.com/arqueon/dankmail/core/internal/keyring"
	"github.com/arqueon/dankmail/core/internal/oauth"
	"github.com/arqueon/dankmail/core/internal/provider"
	"github.com/arqueon/dankmail/core/internal/provider/gmail"
	"github.com/arqueon/dankmail/core/internal/provider/imap"
	"github.com/arqueon/dankmail/core/internal/provider/microsoft"
)

// registry builds and caches one provider instance per account. rebuild
// is called at daemon start and on system.reload (account added/removed
// or re-authenticated).
type registry struct {
	cfg *config.Config
	db  *ent.Client

	mu        gosync.Mutex
	providers map[uuid.UUID]provider.Provider
	// clients caches the OAuth-authenticated HTTP client per gmail
	// account (shared by the mail provider and the People API fetcher).
	clients map[uuid.UUID]*http.Client
}

// Client returns the account's OAuth HTTP client, when it has one.
func (r *registry) Client(id uuid.UUID) (*http.Client, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[id]
	return c, ok
}

func newRegistry(cfg *config.Config, db *ent.Client) *registry {
	return &registry{cfg: cfg, db: db, providers: map[uuid.UUID]provider.Provider{}, clients: map[uuid.UUID]*http.Client{}}
}

func (r *registry) Provider(id uuid.UUID) (provider.Provider, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.providers[id]
	return p, ok
}

// rebuild re-reads the accounts table and reconstructs providers.
// Accounts whose credentials cannot be loaded are flagged auth_error and
// skipped; the rest keep working.
func (r *registry) rebuild(ctx context.Context) error {
	accts, err := r.db.Account.Query().All(ctx)
	if err != nil {
		return err
	}
	next := map[uuid.UUID]provider.Provider{}
	for _, a := range accts {
		p, err := r.build(ctx, a, accts)
		if err != nil {
			slog.Warn("provider unavailable", "account", a.Email, "err", err)
			_, _ = r.db.Account.UpdateOneID(a.ID).
				SetStatus(account.StatusAuthError).
				SetLastError(err.Error()).
				Save(ctx)
			continue
		}
		// Un-park IMAP accounts that were stored while the IMAP sync
		// provider was still pending in an older release.
		if a.Type == account.TypeImap && a.Status == account.StatusPaused && strings.Contains(a.LastError, "next ring") {
			_, _ = r.db.Account.UpdateOneID(a.ID).
				SetStatus(account.StatusActive).
				SetLastError("").
				Save(ctx)
		}
		next[a.ID] = p
	}
	r.mu.Lock()
	r.providers = next
	r.mu.Unlock()
	return nil
}

func accountIDStringsByType(accts []*ent.Account, t account.Type) []string {
	out := make([]string, 0, len(accts))
	for _, a := range accts {
		if a.Type == t {
			out = append(out, a.ID.String())
		}
	}
	return out
}

func (r *registry) build(ctx context.Context, a *ent.Account, all []*ent.Account) (provider.Provider, error) {
	switch a.Type {
	case account.TypeGmail:
		explicit, _ := oauth.LoadClientCreds(a.ID.String())
		creds, err := oauth.ResolveGoogleClient(
			explicit,
			r.cfg.GoogleClientID,
			r.cfg.GoogleClientSecret,
			accountIDStringsByType(all, account.TypeGmail)...,
		)
		if err != nil {
			return nil, err
		}
		broker := oauth.NewBroker(creds.ClientID, creds.ClientSecret, r.cfg.OAuthBindAddr)
		hc, err := broker.Client(ctx, a.ID.String())
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.clients[a.ID] = hc
		r.mu.Unlock()
		opts := gmail.Options{BodyCapBytes: r.cfg.BodyCapKB * 1024}
		if labels, ok := a.Config["labels"].([]any); ok {
			for _, l := range labels {
				if s, ok := l.(string); ok {
					opts.MonitoredLabels = append(opts.MonitoredLabels, s)
				}
			}
		}
		return gmail.NewWithClient(a.ID.String(), a.Email, hc, opts)
	case account.TypeMicrosoft:
		explicit, _ := oauth.LoadClientCreds(a.ID.String())
		creds, err := oauth.ResolveMicrosoftClient(
			explicit.ClientID,
			r.cfg.MicrosoftClientID,
			accountIDStringsByType(all, account.TypeMicrosoft)...,
		)
		if err != nil {
			return nil, fmt.Errorf("no OAuth client stored for %s: %w", a.Email, err)
		}
		broker := oauth.NewBrokerFor(oauth.MicrosoftEndpoints, creds.ClientID, "", r.cfg.OAuthBindAddr)
		hc, err := broker.Client(ctx, a.ID.String())
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.clients[a.ID] = hc
		r.mu.Unlock()
		opts := microsoft.Options{BodyCapBytes: r.cfg.BodyCapKB * 1024}
		return microsoft.New(a.ID.String(), a.Email, microsoft.NewClient(hc), opts), nil
	case account.TypeImap:
		imapCfg, err := accounts.IMAPConfigFromMap(a.Config, a.Email)
		if err != nil {
			return nil, err
		}
		pw, err := keyring.Get(a.ID.String(), keyring.KeyIMAPPassword)
		if err != nil || pw == "" {
			return nil, fmt.Errorf("missing IMAP password in keyring for %s", a.Email)
		}
		smtpPW, _ := keyring.Get(a.ID.String(), keyring.KeySMTPPassword)
		return imap.New(a.ID.String(), a.Email, imap.Options{
			Config:       imapCfg,
			Password:     pw,
			SMTPPassword: smtpPW,
			BodyCapBytes: r.cfg.BodyCapKB * 1024,
		}), nil
	default:
		return nil, fmt.Errorf("unknown account type %q", a.Type)
	}
}
