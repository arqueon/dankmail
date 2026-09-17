package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/errdefs"
	"golang.org/x/oauth2"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func testSource(tok *oauth2.Token, transport http.RoundTripper, save func(*oauth2.Token) error) *persistingSource {
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: transport})
	cfg := &oauth2.Config{ClientID: "test", Endpoint: oauth2.Endpoint{TokenURL: "https://auth.invalid/token", AuthStyle: oauth2.AuthStyleInParams}}
	return &persistingSource{ctx: ctx, cfg: cfg, base: cfg.TokenSource(ctx, tok), current: tok, last: *tok, save: save}
}

func TestClientRefreshesRejectedTokenOnce(t *testing.T) {
	for _, stillRejected := range []bool{false, true} {
		t.Run(fmt.Sprint(stillRejected), func(t *testing.T) {
			refreshes, requests, saves := 0, 0, 0
			source := testSource(&oauth2.Token{AccessToken: "rejected", RefreshToken: "refresh", Expiry: time.Now().Add(time.Hour)}, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				refreshes++
				_ = r.ParseForm()
				defer r.Body.Close()
				if r.Form.Get("refresh_token") != "refresh" {
					t.Error("refresh token not preserved")
				}
				return response(200, `{"access_token":"fresh","refresh_token":"rotated","token_type":"Bearer","expires_in":3600}`), nil
			}), func(tok *oauth2.Token) error {
				saves++
				if tok.RefreshToken != "rotated" {
					t.Error("rotated token not saved")
				}
				return nil
			})
			client := &http.Client{Transport: &refreshTransport{source: source, base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				body, _ := io.ReadAll(r.Body)
				_ = r.Body.Close()
				if string(body) != "payload" {
					t.Error("request body changed on retry")
				}
				if requests == 1 || stillRejected {
					return response(401, `{}`), nil
				}
				if r.Header.Get("Authorization") != "Bearer fresh" {
					t.Error("stale access token reused")
				}
				return response(200, `{}`), nil
			})}}
			req, _ := http.NewRequest(http.MethodPost, "https://mail.invalid/messages", strings.NewReader("payload"))
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			want := 200
			if stillRejected {
				want = 401
			}
			if res.StatusCode != want || refreshes != 1 || requests != 2 || saves != 1 {
				t.Fatalf("status=%d refreshes=%d requests=%d saves=%d", res.StatusCode, refreshes, requests, saves)
			}
			if req.Header.Get("Authorization") != "" {
				t.Error("mutated caller headers")
			}
		})
	}
}

func TestClientDoesNotReplayStreamingBody(t *testing.T) {
	requests := 0
	source := testSource(&oauth2.Token{AccessToken: "old", RefreshToken: "refresh", Expiry: time.Now().Add(time.Hour)}, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected token request")
		return nil, nil
	}), func(*oauth2.Token) error { return nil })
	transport := &refreshTransport{source: source, base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		_ = r.Body.Close()
		return response(401, `{}`), nil
	})}
	req, _ := http.NewRequest(http.MethodPost, "https://mail.invalid", io.NopCloser(strings.NewReader("stream")))
	res, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if requests != 1 || res.StatusCode != 401 {
		t.Fatal("replayed streaming request")
	}
}

func TestTokenErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       errdefs.Kind
	}{
		{"revoked", `{"error":"invalid_grant"}`, 400, errdefs.KindAuth},
		{"client", `{"error":"invalid_client"}`, 400, errdefs.KindAuth},
		{"server", `{"error":"server_error"}`, 503, errdefs.KindNetwork},
		{"rate limit", `{"error":"slow_down"}`, 429, errdefs.KindRateLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSource(&oauth2.Token{RefreshToken: "refresh"}, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				r.Body.Close()
				return response(tc.status, tc.body), nil
			}), func(*oauth2.Token) error { t.Fatal("saved failed refresh"); return nil })
			_, err := s.Token()
			if errdefs.KindOf(err) != tc.want {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	s := testSource(&oauth2.Token{}, nil, nil)
	if _, err := s.Token(); errdefs.KindOf(err) != errdefs.KindAuth {
		t.Fatalf("missing refresh: %v", err)
	}
	if errdefs.KindOf(classifyTokenError(context.Canceled)) != errdefs.KindNetwork {
		t.Fatal("cancellation requires consent")
	}
}

func TestTokenPersistenceRetriesAndConcurrentRefresh(t *testing.T) {
	var refreshes, saves atomic.Int32
	s := testSource(&oauth2.Token{RefreshToken: "refresh"}, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		refreshes.Add(1)
		r.Body.Close()
		return response(200, `{"access_token":"fresh","token_type":"Bearer","expires_in":3600}`), nil
	}), func(tok *oauth2.Token) error {
		if tok.RefreshToken != "refresh" {
			t.Error("lost refresh token when response omitted it")
		}
		if saves.Add(1) == 1 {
			return errors.New("keyring temporarily unavailable")
		}
		return nil
	})
	if _, err := s.Token(); errdefs.KindOf(err) != errdefs.KindNetwork {
		t.Fatalf("save failure: %v", err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := s.Token()
			if err != nil || tok.AccessToken != "fresh" {
				t.Errorf("token: %v", err)
			}
			// A late 401 for an older token must not invalidate the fresh one.
			s.invalidate("older")
		}()
	}
	wg.Wait()
	if refreshes.Load() != 1 || saves.Load() != 2 {
		t.Fatalf("refreshes=%d saves=%d", refreshes.Load(), saves.Load())
	}
}
