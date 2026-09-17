package gmail

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/errdefs"
	"google.golang.org/api/googleapi"
)

func TestReloadPreservesAccountCooldownAndPacing(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/limited/gmail/v1/users/me/profile" {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"code":429}}`))
			return
		}
		_, _ = w.Write([]byte(`{"emailAddress":"test@example.com","historyId":"1"}`))
	}))
	defer server.Close()
	id := t.Name()
	q, sleeps := fakeQuota()
	accountQuotas.Store(id, q)
	t.Cleanup(func() { accountQuotas.Delete(id); accountQuotas.Delete(id + "-other") })
	build := func(account string, limited bool) *realAPI {
		p, err := NewWithClient(account, "test@example.com", server.Client(), Options{})
		if err != nil {
			t.Fatal(err)
		}
		api := p.api.(*realAPI)
		api.svc.BasePath = server.URL + "/"
		if limited {
			api.svc.BasePath += "limited/"
		}
		return api
	}
	first := build(id, false)
	if _, _, err := first.GetProfile(context.Background()); err != nil {
		t.Fatal(err)
	}
	rebuilt := build(id, false)
	if _, _, err := rebuilt.GetProfile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*sleeps) != 1 || (*sleeps)[0] != time.Second/quotaUnitsPerSecond {
		t.Fatalf("pacing lost across reload: %v", *sleeps)
	}
	limited := build(id, true)
	_, _, err := limited.GetProfile(context.Background())
	if delay := errdefs.RetryAfter(err); delay < time.Hour || delay > time.Hour+time.Second {
		t.Fatalf("missing Retry-After: %v (%v)", err, delay)
	}
	if calls.Load() != 3 {
		t.Fatalf("long wait retried inline: %d requests", calls.Load())
	}
	reloaded := build(id, false)
	_, _, err = reloaded.GetProfile(context.Background())
	if errdefs.RetryAfter(err) < time.Hour || calls.Load() != 3 {
		t.Fatalf("reload forgot cooldown: calls=%d err=%v", calls.Load(), err)
	}
	other := build(id+"-other", false)
	if _, _, err := other.GetProfile(context.Background()); err != nil {
		t.Fatalf("other account blocked: %v", err)
	}
	for _, delay := range *sleeps {
		if delay > maxInlineDelay {
			t.Fatalf("long inline sleep: %v", delay)
		}
	}
}

func TestSyncRetryBudgetSpansHistoryPages(t *testing.T) {
	q, sleeps := fakeQuota()
	attempts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		page := req.URL.Query().Get("pageToken")
		attempts[page]++
		w.Header().Set("Content-Type", "application/json")
		if attempts[page] == 1 {
			q.cooldown(2*time.Second, &googleapi.Error{Code: 503})
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"error":{"code":503}}`))
			return
		}
		switch page {
		case "":
			_, _ = w.Write([]byte(`{"historyId":"12","nextPageToken":"two"}`))
		case "two":
			_, _ = w.Write([]byte(`{"historyId":"12","nextPageToken":"three"}`))
		default:
			_, _ = w.Write([]byte(`{"historyId":"12"}`))
		}
	}))
	defer server.Close()
	api, err := newRealAPI(server.Client())
	if err != nil {
		t.Fatal(err)
	}
	api.svc.BasePath = server.URL + "/"
	api.quota = q
	p := New("test", "test@example.com", api, Options{})
	changes, cursor, err := p.Sync(context.Background(), cursorV2Prefix+"10")
	var deferred *deferredRetry
	if !errors.As(err, &deferred) || attempts[""] != 2 || attempts["two"] != 2 || attempts["three"] != 1 {
		t.Fatalf("budget reset per page: attempts=%v err=%v", attempts, err)
	}
	if cursor != "" || len(changes.Upserted) != 0 || changes.FullResync {
		t.Fatal("incomplete sync must not advance cursor or reconcile a partial snapshot")
	}
	var retryWait time.Duration
	for _, d := range *sleeps {
		if d >= time.Second {
			retryWait += d
		}
	}
	if retryWait != 4*time.Second {
		t.Fatalf("retry sleep = %s, want 4s before deferring third page", retryWait)
	}
	// A later operation gets a fresh budget; it does not inherit the previous
	// operation's spent budget simply because the Provider is cached.
	q.sleep(context.Background(), deferred.RetryAfter())
	if _, _, err := p.Sync(context.Background(), cursorV2Prefix+"10"); err != nil {
		t.Fatalf("new operation inherited exhausted budget: %v", err)
	}
}

func TestWriteCooldownIsSharedWithoutReplayingSend(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":429}}`))
	}))
	defer server.Close()
	api, err := newRealAPI(server.Client())
	if err != nil {
		t.Fatal(err)
	}
	api.svc.BasePath = server.URL + "/"
	api.quota, _ = fakeQuota()
	if err := api.SendMessage(context.Background(), "", []byte("test")); errdefs.RetryAfter(err) < time.Hour {
		t.Fatalf("send lost cooldown: %v", err)
	}
	if _, _, err := api.GetProfile(context.Background()); errdefs.RetryAfter(err) < time.Hour {
		t.Fatalf("read ignored send cooldown: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("requests=%d, want one send and no replay or read during cooldown", calls.Load())
	}
}
