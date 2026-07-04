package oauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/arqueon/dankmail/core/errdefs"
)

// Flow is one in-progress OAuth consent: the loopback listener is up and
// the consent URL built, but the browser has not necessarily been opened.
// The GUI wizard opens AuthURL itself and then calls Wait; the CLI does
// both via Broker.Authorize.
type Flow struct {
	cfg      *oauth2.Config
	verifier string
	state    string
	authURL  string
	ln       net.Listener
	srv      *http.Server
	resCh    chan flowResult
}

type flowResult struct {
	code string
	err  error
}

// StartFlow binds the loopback listener and returns the pending flow.
func (b *Broker) StartFlow() (*Flow, error) {
	ln, err := net.Listen("tcp", b.bindAddr)
	if err != nil {
		return nil, err
	}
	state, err := randomState()
	if err != nil {
		_ = ln.Close()
		return nil, err
	}

	f := &Flow{
		cfg:      b.config(fmt.Sprintf("http://%s/callback", ln.Addr().String())),
		verifier: oauth2.GenerateVerifier(),
		state:    state,
		ln:       ln,
		resCh:    make(chan flowResult, 1),
	}
	f.authURL = f.cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(f.verifier),
		// Force the refresh-token grant even on re-consent.
		oauth2.SetAuthURLParam("prompt", "consent"),
	)

	f.srv = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		switch {
		case q.Get("state") != state:
			http.Error(w, "state mismatch", http.StatusBadRequest)
			f.deliver(flowResult{err: errors.New("oauth: state mismatch")})
		case q.Get("error") != "":
			fmt.Fprintln(w, "Autorización denegada. Puedes cerrar esta pestaña.")
			f.deliver(flowResult{err: fmt.Errorf("oauth: consent denied: %s", q.Get("error"))})
		default:
			fmt.Fprintln(w, "Cuenta autorizada. Puedes cerrar esta pestaña y volver a dankmail.")
			f.deliver(flowResult{code: q.Get("code")})
		}
	})}
	go func() { _ = f.srv.Serve(ln) }()
	return f, nil
}

func (f *Flow) deliver(res flowResult) {
	select {
	case f.resCh <- res:
	default:
	}
}

// AuthURL is the consent page to open in the user's browser.
func (f *Flow) AuthURL() string { return f.authURL }

// State identifies this flow across IPC calls.
func (f *Flow) State() string { return f.state }

// Wait blocks until the redirect lands (or ctx ends) and exchanges the
// code. The flow is closed either way.
func (f *Flow) Wait(ctx context.Context) (*oauth2.Token, error) {
	defer f.Close()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-f.resCh:
		if res.err != nil {
			return nil, res.err
		}
		tok, err := f.cfg.Exchange(ctx, res.code, oauth2.VerifierOption(f.verifier))
		if err != nil {
			return nil, errdefs.Wrap(errdefs.KindAuth, err)
		}
		return tok, nil
	}
}

// Close tears down the loopback listener.
func (f *Flow) Close() {
	shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = f.srv.Shutdown(shutCtx)
}
