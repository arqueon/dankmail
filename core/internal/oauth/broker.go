package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"time"

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

// Authorize opens the browser, waits for the loopback redirect, and
// exchanges the code (PKCE + state). Blocks until consent or ctx done.
func (b *Broker) Authorize(ctx context.Context) (*oauth2.Token, error) {
	ln, err := net.Listen("tcp", b.bindAddr)
	if err != nil {
		return nil, err
	}
	defer ln.Close()

	redirect := fmt.Sprintf("http://%s/callback", ln.Addr().String())
	cfg := b.config(redirect)
	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	type result struct {
		code string
		err  error
	}
	resCh := make(chan result, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		switch {
		case q.Get("state") != state:
			http.Error(w, "state mismatch", http.StatusBadRequest)
			resCh <- result{err: errors.New("oauth: state mismatch")}
		case q.Get("error") != "":
			fmt.Fprintln(w, "Autorización denegada. Puedes cerrar esta pestaña.")
			resCh <- result{err: fmt.Errorf("oauth: consent denied: %s", q.Get("error"))}
		default:
			fmt.Fprintln(w, "Cuenta autorizada. Puedes cerrar esta pestaña y volver a dankmail.")
			resCh <- result{code: q.Get("code")}
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	authURL := cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
		// Force the refresh-token grant even on re-consent.
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	if err := b.openURL(authURL); err != nil {
		return nil, fmt.Errorf("oauth: cannot open browser (visit manually: %s): %w", authURL, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		if res.err != nil {
			return nil, res.err
		}
		tok, err := cfg.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return nil, errdefs.Wrap(errdefs.KindAuth, err)
		}
		return tok, nil
	}
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
