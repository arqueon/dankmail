package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"time"

	entaccount "github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/internal/accounts"
	"github.com/arqueon/dankmail/core/internal/ipc"
	"github.com/arqueon/dankmail/core/internal/keyring"
	"github.com/arqueon/dankmail/core/internal/oauth"
)

// flowRegistry tracks pending OAuth consents between the "start" and
// "complete" IPC calls of the account wizard (pattern from dankcalendar).
type flowRegistry struct {
	mu    gosync.Mutex
	flows map[string]*pendingFlow
}

type pendingFlow struct {
	flow  *oauth.Flow
	creds oauth.ClientCreds
	// kind selects the finisher on complete: "gmail" or "microsoft".
	kind string
}

func newFlowRegistry() *flowRegistry {
	return &flowRegistry{flows: map[string]*pendingFlow{}}
}

// readUserFile reads a file path coming from the GUI: file:// URLs and a
// leading ~ are both accepted so drag-and-drop payloads work verbatim.
func readUserFile(path string) ([]byte, error) {
	path = strings.TrimPrefix(path, "file://")
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, strings.TrimPrefix(path[1:], "/"))
	}
	return os.ReadFile(path)
}

func (r *flowRegistry) register(f *oauth.Flow, creds oauth.ClientCreds, kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flows[f.State()] = &pendingFlow{flow: f, creds: creds, kind: kind}
}

func (r *flowRegistry) take(state string) (*pendingFlow, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.flows[state]
	if ok {
		delete(r.flows, state)
	}
	return p, ok
}

// registerAccountIPC wires the account-wizard surface:
//
//	accounts.gmail.setupGuide → []SetupStep + env-default client creds
//	accounts.gmail.start      → {state, authUrl}; the GUI opens the browser
//	accounts.gmail.complete   → waits for consent, stores account+secrets
//	accounts.reauth           → {state, authUrl} for an existing account
//	                            (stored client creds); completed via
//	                            accounts.gmail.complete like a fresh add
//	accounts.flow.cancel      → abort a pending consent
//	accounts.remove           → drop account + its keyring secrets
func (d *daemon) registerAccountIPC(srv *ipc.Server) {
	srv.Register("accounts.gmail.setupGuide", func(ctx context.Context, _ map[string]any) (any, error) {
		return map[string]any{
			"steps": accounts.GmailSetupSteps(),
			// Pre-fill the creds form when the environment already
			// carries a client (never expose the secret value itself).
			"defaultClientId": d.cfg.GoogleClientID,
			"hasDefaultCreds": d.cfg.GoogleClientID != "" && d.cfg.GoogleClientSecret != "",
		}, nil
	})

	srv.Register("accounts.gmail.start", func(ctx context.Context, p map[string]any) (any, error) {
		creds := oauth.ClientCreds{}
		creds.ClientID, _ = p["clientId"].(string)
		creds.ClientSecret, _ = p["clientSecret"].(string)
		// The wizard may hand over the downloaded client_secret_*.json
		// verbatim instead of the individual fields (dcal parity).
		if raw, _ := p["clientJson"].(string); raw != "" {
			parsed, err := oauth.ParseClientJSON([]byte(raw))
			if err != nil {
				return nil, err
			}
			creds = parsed
		}
		// …or a path to that file (picked or dropped in the GUI).
		if path, _ := p["clientJsonPath"].(string); path != "" {
			raw, err := readUserFile(path)
			if err != nil {
				return nil, err
			}
			parsed, err := oauth.ParseClientJSON(raw)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			creds = parsed
		}
		if creds.ClientID == "" {
			creds.ClientID = d.cfg.GoogleClientID
		}
		if creds.ClientSecret == "" {
			creds.ClientSecret = d.cfg.GoogleClientSecret
		}
		if creds.ClientID == "" || creds.ClientSecret == "" {
			return nil, fmt.Errorf("client ID and secret are required (see the setup guide)")
		}

		broker := oauth.NewBroker(creds.ClientID, creds.ClientSecret, d.cfg.OAuthBindAddr)
		flow, err := broker.StartFlow()
		if err != nil {
			return nil, err
		}
		d.flows.register(flow, creds, "gmail")
		return map[string]any{
			"state":   flow.State(),
			"authUrl": flow.AuthURL(),
		}, nil
	})

	srv.Register("accounts.microsoft.setupGuide", func(ctx context.Context, _ map[string]any) (any, error) {
		return map[string]any{"steps": accounts.MicrosoftSetupSteps()}, nil
	})

	srv.Register("accounts.microsoft.start", func(ctx context.Context, p map[string]any) (any, error) {
		clientID, _ := p["clientId"].(string)
		if clientID == "" {
			return nil, fmt.Errorf("client ID is required (see the setup guide)")
		}
		// Public client: no secret, PKCE carries the proof.
		broker := oauth.NewBrokerFor(oauth.MicrosoftEndpoints, clientID, "", d.cfg.OAuthBindAddr)
		flow, err := broker.StartFlow()
		if err != nil {
			return nil, err
		}
		d.flows.register(flow, oauth.ClientCreds{ClientID: clientID}, "microsoft")
		return map[string]any{
			"state":   flow.State(),
			"authUrl": flow.AuthURL(),
		}, nil
	})

	// accounts.gmail.complete finishes ANY pending consent (the name
	// predates multi-provider; the flow's kind picks the finisher, so
	// the GUI needs a single completion call for add and re-auth alike).
	srv.Register("accounts.gmail.complete", func(ctx context.Context, p map[string]any) (any, error) {
		state, _ := p["state"].(string)
		if state == "" {
			return nil, fmt.Errorf("state is required")
		}
		pending, ok := d.flows.take(state)
		if !ok {
			return nil, fmt.Errorf("no pending flow for that state")
		}

		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		tok, err := pending.flow.Wait(waitCtx)
		if err != nil {
			return nil, err
		}
		var res accounts.Result
		switch pending.kind {
		case "microsoft":
			res, err = accounts.FinishMicrosoft(ctx, d.db, pending.creds, tok)
		default:
			res, err = accounts.FinishGmail(ctx, d.db, pending.creds, tok)
		}
		if err != nil {
			return nil, err
		}
		d.requestReload()
		d.bus.Publish("accounts.changed", map[string]any{"accountId": res.AccountID})
		return res, nil
	})

	srv.Register("accounts.reauth", func(ctx context.Context, p map[string]any) (any, error) {
		idStr, _ := p["id"].(string)
		id, err := parseUUID(idStr)
		if err != nil {
			return nil, fmt.Errorf("bad account id")
		}
		acct, err := d.db.Account.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		// Prefer the OAuth client stored with the account at add time,
		// falling back to env-provided creds (same order as the CLI).
		creds, err := oauth.LoadClientCreds(idStr)
		if err != nil {
			creds = oauth.ClientCreds{}
		}
		var broker *oauth.Broker
		var kind string
		switch acct.Type {
		case entaccount.TypeGmail:
			if creds.ClientID == "" {
				creds = oauth.ClientCreds{ClientID: d.cfg.GoogleClientID, ClientSecret: d.cfg.GoogleClientSecret}
			}
			if creds.ClientID == "" || creds.ClientSecret == "" {
				return nil, fmt.Errorf("no OAuth client stored for this account; re-add it via the wizard")
			}
			broker = oauth.NewBroker(creds.ClientID, creds.ClientSecret, d.cfg.OAuthBindAddr)
			kind = "gmail"
		case entaccount.TypeMicrosoft:
			if creds.ClientID == "" {
				return nil, fmt.Errorf("no OAuth client stored for this account; re-add it via the wizard")
			}
			broker = oauth.NewBrokerFor(oauth.MicrosoftEndpoints, creds.ClientID, "", d.cfg.OAuthBindAddr)
			kind = "microsoft"
		default:
			return nil, fmt.Errorf("re-authentication applies to OAuth accounts only; re-add the account to change an IMAP password")
		}
		flow, err := broker.StartFlow()
		if err != nil {
			return nil, err
		}
		d.flows.register(flow, creds, kind)
		return map[string]any{
			"state":   flow.State(),
			"authUrl": flow.AuthURL(),
		}, nil
	})

	srv.Register("accounts.flow.cancel", func(ctx context.Context, p map[string]any) (any, error) {
		state, _ := p["state"].(string)
		if state == "" {
			return nil, fmt.Errorf("state is required")
		}
		pending, ok := d.flows.take(state)
		if ok {
			pending.flow.Close()
		}
		return map[string]any{"cancelled": ok}, nil
	})

	srv.Register("accounts.imap.presets", func(ctx context.Context, _ map[string]any) (any, error) {
		return accounts.IMAPPresets(), nil
	})

	srv.Register("accounts.imap.add", func(ctx context.Context, p map[string]any) (any, error) {
		email, _ := p["email"].(string)
		password, _ := p["password"].(string)
		cfg := accounts.IMAPConfig{}
		cfg.Host, _ = p["host"].(string)
		if port, ok := p["port"].(float64); ok {
			cfg.Port = int(port)
		}
		cfg.Security, _ = p["security"].(string)
		cfg.Username, _ = p["username"].(string)
		cfg.SMTPHost, _ = p["smtpHost"].(string)
		if port, ok := p["smtpPort"].(float64); ok {
			cfg.SMTPPort = int(port)
		}
		cfg.WebmailURL, _ = p["webmailUrl"].(string)

		res, err := accounts.AddIMAP(ctx, d.db, cfg, email, password)
		if err != nil {
			return nil, err
		}
		d.requestReload()
		d.bus.Publish("accounts.changed", map[string]any{"accountId": res.AccountID})
		return res, nil
	})

	srv.Register("accounts.remove", func(ctx context.Context, p map[string]any) (any, error) {
		idStr, _ := p["id"].(string)
		id, err := parseUUID(idStr)
		if err != nil {
			return nil, fmt.Errorf("bad account id")
		}
		if err := d.db.Account.DeleteOneID(id).Exec(ctx); err != nil {
			return nil, err
		}
		for _, key := range []string{keyring.KeyOAuthToken, keyring.KeyOAuthClient, keyring.KeyIMAPPassword, keyring.KeySMTPPassword} {
			_ = keyring.Delete(idStr, key)
		}
		d.requestReload()
		d.bus.Publish("accounts.changed", map[string]any{"accountId": idStr})
		return "ok", nil
	})
}
