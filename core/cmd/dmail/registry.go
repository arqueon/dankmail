package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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

// errProviderPending marks account types whose sync provider is not
// shipped yet (IMAP: anillo 2). The account is stored and parked.
var errProviderPending = errors.New("sync provider for this account type arrives in the next ring; account parked (paused)")

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
		p, err := r.build(ctx, a)
		switch {
		case errors.Is(err, errProviderPending):
			// Account type stored but its sync provider isn't shipped
			// yet: park it (paused, not an auth failure).
			if a.Status == account.StatusActive {
				_, _ = r.db.Account.UpdateOneID(a.ID).
					SetStatus(account.StatusPaused).
					SetLastError(err.Error()).
					Save(ctx)
			}
			continue
		case err != nil:
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
		hc := oauth2.NewClient(ctx, ts)
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
	case account.TypeImap:
		return nil, errProviderPending
	default:
		return nil, fmt.Errorf("unknown account type %q", a.Type)
	}
}
