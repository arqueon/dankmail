package oauth

import (
	"context"
	"io"
	"net/http"

	"golang.org/x/oauth2"
)

// Client renews a rejected access token once before requiring consent.
// It shares one token source between mail and contacts requests.
func (b *Broker) Client(ctx context.Context, accountID string) (*http.Client, error) {
	source, err := b.tokenSource(ctx, accountID)
	if err != nil {
		return nil, err
	}
	client := *oauth2.NewClient(ctx, nil)
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = &refreshTransport{source: source, base: base}
	return &client, nil
}

type refreshTransport struct {
	source *persistingSource
	base   http.RoundTripper
}

func (t *refreshTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.source.Token()
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	authed := req.Clone(req.Context())
	tok.SetAuthHeader(authed)
	res, err := t.base.RoundTrip(authed)
	if err != nil || res.StatusCode != http.StatusUnauthorized {
		return res, err
	}
	t.source.invalidate(tok.AccessToken)
	// Never replay a body we cannot reconstruct, or retry in a loop.
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return res, nil
	}
	if req.Context().Err() != nil {
		return res, nil
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		retry.Body, err = req.GetBody()
		if err != nil {
			return res, nil
		}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	_ = res.Body.Close()
	tok, err = t.source.Token()
	if err != nil {
		if retry.Body != nil {
			_ = retry.Body.Close()
		}
		return nil, err
	}
	tok.SetAuthHeader(retry)
	return t.base.RoundTrip(retry)
}
