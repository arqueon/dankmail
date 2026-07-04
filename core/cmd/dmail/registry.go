package main

import (
	"context"
	"fmt"
	"log/slog"
	gosync "sync"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/arqueon/dankmail/core/config"
	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/internal/oauth"
	"github.com/arqueon/dankmail/core/internal/provider"
	"github.com/arqueon/dankmail/core/internal/provider/gmail"
)

// registry builds and caches one provider instance per account. rebuild
// is called at daemon start and on system.reload (account added/removed
// or re-authenticated).
type registry struct {
	cfg *config.Config
	db  *ent.Client

	mu        gosync.Mutex
	providers map[uuid.UUID]provider.Provider
}

func newRegistry(cfg *config.Config, db *ent.Client) *registry {
	return &registry{cfg: cfg, db: db, providers: map[uuid.UUID]provider.Provider{}}
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
		p, err := r.build(ctx, a)
		if err != nil {
			slog.Warn("provider unavailable", "account", a.Email, "err", err)
			_, _ = r.db.Account.UpdateOneID(a.ID).
				SetStatus(account.StatusAuthError).
				SetLastError(err.Error()).
				Save(ctx)
			continue
		}
		next[a.ID] = p
	}
	r.mu.Lock()
	r.providers = next
	r.mu.Unlock()
	return nil
}

func (r *registry) build(ctx context.Context, a *ent.Account) (provider.Provider, error) {
	switch a.Type {
	case account.TypeGmail:
		// The account's own OAuth client lives in the keyring (stored at
		// add time); the environment is only a fallback for accounts
		// added before that existed.
		creds, err := oauth.LoadClientCreds(a.ID.String())
		if err != nil || creds.ClientID == "" {
			creds = oauth.ClientCreds{ClientID: r.cfg.GoogleClientID, ClientSecret: r.cfg.GoogleClientSecret}
		}
		broker := oauth.NewBroker(creds.ClientID, creds.ClientSecret, r.cfg.OAuthBindAddr)
		ts, err := broker.TokenSource(ctx, a.ID.String())
		if err != nil {
			return nil, err
		}
		opts := gmail.Options{BodyCapBytes: r.cfg.BodyCapKB * 1024}
		if labels, ok := a.Config["labels"].([]any); ok {
			for _, l := range labels {
				if s, ok := l.(string); ok {
					opts.MonitoredLabels = append(opts.MonitoredLabels, s)
				}
			}
		}
		return gmail.NewWithClient(a.ID.String(), a.Email, oauth2.NewClient(ctx, ts), opts)
	case account.TypeImap:
		return nil, fmt.Errorf("imap provider lands in anillo 2")
	default:
		return nil, fmt.Errorf("unknown account type %q", a.Type)
	}
}
