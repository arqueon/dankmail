package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/microsoft"

	"github.com/arqueon/dankmail/core/errdefs"
	"github.com/arqueon/dankmail/core/internal/keyring"
)

// Endpoints selects the identity provider a Broker talks to. The flow
// itself (loopback listener, state, PKCE) is provider-agnostic.
type Endpoints struct {
	Endpoint     oauth2.Endpoint
	Scopes       []string
	RedirectHost string
}

var (
	// GoogleEndpoints is the Gmail desktop client (has a pseudo-secret).
	GoogleEndpoints = Endpoints{Endpoint: google.Endpoint, Scopes: GmailScopes}
	// MicrosoftEndpoints is the Graph public client (tenant "common"
	// covers personal + organizational accounts; no client secret —
	// PKCE carries the proof). offline_access yields the refresh token.
	MicrosoftEndpoints = Endpoints{
		Endpoint:     microsoft.AzureADEndpoint("common"),
		Scopes:       GraphScopes,
		RedirectHost: "localhost",
	}
)

// Broker runs the OAuth 2.0 desktop (loopback) flow and persists tokens
// in the system keyring, keyed by account ID.
type Broker struct {
	endpoints    Endpoints
	clientID     string
	clientSecret string
	bindAddr     string // loopback listener, e.g. "127.0.0.1:0"

	// openURL opens the consent page in the user's browser; seam for tests.
	openURL func(url string) error
}

// NewBroker keeps the historical Google-fixed constructor; provider-aware
// callers use NewBrokerFor.
func NewBroker(clientID, clientSecret, bindAddr string) *Broker {
	return NewBrokerFor(GoogleEndpoints, clientID, clientSecret, bindAddr)
}

func NewBrokerFor(ep Endpoints, clientID, clientSecret, bindAddr string) *Broker {
	return &Broker{
		endpoints:    ep,
		clientID:     clientID,
		clientSecret: clientSecret,
		bindAddr:     bindAddr,
		openURL: func(url string) error {
			return exec.Command("xdg-open", url).Start()
		},
	}
}

func (b *Broker) config(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     b.clientID,
		ClientSecret: b.clientSecret,
		Endpoint:     b.endpoints.Endpoint,
		Scopes:       b.endpoints.Scopes,
		RedirectURL:  redirectURL,
	}
}

// Authorize is the CLI path: start a flow, open the browser, wait.
func (b *Broker) Authorize(ctx context.Context) (*oauth2.Token, error) {
	flow, err := b.StartFlow()
	if err != nil {
		return nil, err
	}
	if err := b.openURL(flow.AuthURL()); err != nil {
		flow.Close()
		return nil, fmt.Errorf("oauth: cannot open browser (visit manually: %s): %w", flow.AuthURL(), err)
	}
	return flow.Wait(ctx)
}

// ClientCreds is the user-supplied OAuth desktop client, stored in the
// keyring next to the token so refresh works regardless of environment.
type ClientCreds struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// ParseClientJSON extracts the credentials from the client_secret_*.json
// file Google Console offers for download. Only the "installed"
// (Desktop app) shape is accepted — "web" clients cannot do the
// loopback flow.
func ParseClientJSON(raw []byte) (ClientCreds, error) {
	var doc struct {
		Installed *struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
		Web *struct{} `json:"web"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ClientCreds{}, fmt.Errorf("not a Google client JSON: %w", err)
	}
	if doc.Web != nil {
		return ClientCreds{}, fmt.Errorf("this is a \"web application\" client; create one of type \"Desktop app\" instead")
	}
	if doc.Installed == nil || doc.Installed.ClientID == "" || doc.Installed.ClientSecret == "" {
		return ClientCreds{}, fmt.Errorf("client JSON is missing the installed.client_id/client_secret fields")
	}
	return ClientCreds{ClientID: doc.Installed.ClientID, ClientSecret: doc.Installed.ClientSecret}, nil
}

func SaveClientCreds(accountID string, c ClientCreds) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return keyring.Set(accountID, keyring.KeyOAuthClient, string(raw))
}

func LoadClientCreds(accountID string) (ClientCreds, error) {
	raw, err := keyring.Get(accountID, keyring.KeyOAuthClient)
	if err != nil {
		return ClientCreds{}, err
	}
	var c ClientCreds
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return ClientCreds{}, err
	}
	return c, nil
}

// SaveToken persists tok in the keyring for accountID.
func SaveToken(accountID string, tok *oauth2.Token) error {
	raw, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	return keyring.Set(accountID, keyring.KeyOAuthToken, string(raw))
}

// LoadToken retrieves the stored token.
func LoadToken(accountID string) (*oauth2.Token, error) {
	raw, err := keyring.Get(accountID, keyring.KeyOAuthToken)
	if err != nil {
		return nil, errdefs.Wrap(errdefs.KindAuth, err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(raw), &tok); err != nil {
		return nil, errdefs.Wrap(errdefs.KindAuth, err)
	}
	return &tok, nil
}

// TokenSource returns an auto-refreshing source for the account that
// persists refreshed tokens back to the keyring.
func (b *Broker) TokenSource(ctx context.Context, accountID string) (oauth2.TokenSource, error) {
	return b.tokenSource(ctx, accountID)
}

func (b *Broker) tokenSource(ctx context.Context, accountID string) (*persistingSource, error) {
	tok, err := LoadToken(accountID)
	if err != nil {
		return nil, err
	}
	cfg := b.config("")
	return &persistingSource{
		ctx: ctx, cfg: cfg, base: cfg.TokenSource(ctx, tok), current: tok,
		last: *tok, save: func(t *oauth2.Token) error { return SaveToken(accountID, t) },
	}, nil
}

type persistingSource struct {
	mu      sync.Mutex
	ctx     context.Context
	cfg     *oauth2.Config
	base    oauth2.TokenSource
	current *oauth2.Token
	last    oauth2.Token
	save    func(*oauth2.Token) error
}

func (s *persistingSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.current.Valid() && s.current.RefreshToken == "" {
		return nil, errdefs.Wrap(errdefs.KindAuth, errors.New("oauth: refresh token is missing"))
	}
	tok, err := s.base.Token()
	if err != nil {
		return nil, classifyTokenError(err)
	}
	s.current = tok
	if tok.AccessToken != s.last.AccessToken || tok.RefreshToken != s.last.RefreshToken ||
		tok.TokenType != s.last.TokenType || !tok.Expiry.Equal(s.last.Expiry) {
		if err := s.save(tok); err != nil {
			// Leave last untouched so a later request retries persistence.
			return nil, errdefs.Wrap(errdefs.KindNetwork, fmt.Errorf("oauth: save refreshed token: %w", err))
		}
		s.last = *tok
	}
	return tok, nil
}

// invalidate discards only the rejected access token. Concurrent requests
// that already refreshed it must not trigger another refresh.
func (s *persistingSource) invalidate(rejected string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current.AccessToken != rejected {
		return
	}
	tok := &oauth2.Token{RefreshToken: s.current.RefreshToken}
	s.current = tok
	s.base = s.cfg.TokenSource(s.ctx, tok)
}

func classifyTokenError(err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		switch re.ErrorCode {
		case "invalid_grant", "invalid_client", "unauthorized_client", "access_denied", "interaction_required", "login_required", "consent_required":
			return errdefs.Wrap(errdefs.KindAuth, err)
		}
		if re.Response != nil {
			switch re.Response.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				return errdefs.Wrap(errdefs.KindAuth, err)
			case http.StatusTooManyRequests:
				return errdefs.Wrap(errdefs.KindRateLimit, err)
			}
		}
	}
	return errdefs.Wrap(errdefs.KindNetwork, err)
}

func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
