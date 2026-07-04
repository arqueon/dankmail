package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/arqueon/dankmail/core/errdefs"
	"github.com/arqueon/dankmail/core/internal/keyring"
)

// Broker runs the OAuth 2.0 desktop (loopback) flow and persists tokens
// in the system keyring, keyed by account ID.
type Broker struct {
	clientID     string
	clientSecret string
	bindAddr     string // loopback listener, e.g. "127.0.0.1:0"

	// openURL opens the consent page in the user's browser; seam for tests.
	openURL func(url string) error
}

func NewBroker(clientID, clientSecret, bindAddr string) *Broker {
	return &Broker{
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
		Endpoint:     google.Endpoint,
		Scopes:       GmailScopes,
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
	tok, err := LoadToken(accountID)
	if err != nil {
		return nil, err
	}
	base := b.config("").TokenSource(ctx, tok)
	return &persistingSource{accountID: accountID, base: base, last: tok.AccessToken}, nil
}

type persistingSource struct {
	accountID string
	base      oauth2.TokenSource
	last      string
}

func (s *persistingSource) Token() (*oauth2.Token, error) {
	tok, err := s.base.Token()
	if err != nil {
		return nil, errdefs.Wrap(errdefs.KindAuth, err)
	}
	if tok.AccessToken != s.last {
		s.last = tok.AccessToken
		if err := SaveToken(s.accountID, tok); err != nil {
			return nil, err
		}
	}
	return tok, nil
}

func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
