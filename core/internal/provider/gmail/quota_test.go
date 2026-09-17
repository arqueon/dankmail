package gmail

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
)

func fakeQuota() (*quotaGate, *[]time.Duration) {
	now := time.Unix(0, 0)
	var sleeps []time.Duration
	q := &quotaGate{now: func() time.Time { return now }}
	q.sleep = func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		sleeps = append(sleeps, d)
		now = now.Add(d)
		return nil
	}
	return q, &sleeps
}

func TestQuotaPacesByCostAndHonorsCooldown(t *testing.T) {
	q, sleeps := fakeQuota()
	ctx := context.Background()
	for _, cost := range []int{40, 20, 10} {
		if err := q.wait(ctx, cost); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(*sleeps, []time.Duration{time.Second, time.Second / 2}) {
		t.Fatalf("waits %v", *sleeps)
	}
	q.cooldown(time.Minute)
	q.cooldown(time.Second)
	if err := q.wait(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got := (*sleeps)[2]; got != time.Minute {
		t.Fatalf("cooldown shortened: %v", got)
	}
}

func TestReadRetryDelay(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	cases := []struct {
		name  string
		err   error
		want  time.Duration
		retry bool
	}{
		{"quota", &googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "rateLimitExceeded"}}}, time.Minute, true},
		{"user quota", &googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "userRateLimitExceeded"}}}, time.Minute, true},
		{"429", &googleapi.Error{Code: 429}, time.Minute, true},
		{"server", &googleapi.Error{Code: 503}, time.Second, true},
		{"retry seconds", &googleapi.Error{Code: 429, Header: http.Header{"Retry-After": []string{"180"}}}, 3 * time.Minute, true},
		{"retry date", &googleapi.Error{Code: 429, Header: http.Header{"Retry-After": []string{now.Add(4 * time.Minute).Format(http.TimeFormat)}}}, 4 * time.Minute, true},
		{"permission", &googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "insufficientPermissions"}}}, 0, false},
		{"auth", &googleapi.Error{Code: 401}, 0, false},
		{"not found", &googleapi.Error{Code: 404}, 0, false},
		{"network", errors.New("offline"), 0, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			d, retry := readRetryDelay(tt.err, 0, now)
			if d != tt.want || retry != tt.retry {
				t.Fatalf("got %v,%v", d, retry)
			}
		})
	}
	if d, _ := readRetryDelay(&googleapi.Error{Code: 429}, 4, now); d != 2*time.Minute {
		t.Fatalf("backoff %v", d)
	}
}

func TestReadRetriesAreBoundedAndCancellable(t *testing.T) {
	q, _ := fakeQuota()
	r := &realAPI{quota: q}
	calls := 0
	_, err := readCall(context.Background(), r, 40, func() (*int, error) { calls++; return nil, &googleapi.Error{Code: 429} })
	if err == nil || calls != maxReadRetries+1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls = 0
	q, _ = fakeQuota()
	r.quota = q
	_, err = readCall(ctx, r, 40, func() (*int, error) { calls++; cancel(); return nil, &googleapi.Error{Code: 429} })
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestGmailSyncRetriesOnlyFailedThread(t *testing.T) {
	var paths []string
	failed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/gmail/v1/users/me/history":
			_, _ = w.Write([]byte(`{"historyId":"12","history":[{"id":"12","messagesAdded":[{"message":{"id":"a","threadId":"a"}},{"message":{"id":"b","threadId":"b"}}]}]}`))
		case "/gmail/v1/users/me/threads/b":
			if !failed {
				failed = true
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"error":{"code":403,"errors":[{"reason":"rateLimitExceeded"}]}}`))
				return
			}
			fallthrough
		case "/gmail/v1/users/me/threads/a":
			_, _ = w.Write([]byte(`{"id":"` + req.URL.Path[len(req.URL.Path)-1:] + `","messages":[{"id":"m","labelIds":["INBOX"],"payload":{"mimeType":"text/plain"}}]}`))
		default:
			t.Errorf("unexpected path %s", req.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	api, err := newRealAPI(server.Client())
	if err != nil {
		t.Fatal(err)
	}
	api.svc.BasePath = server.URL + "/"
	api.quota, _ = fakeQuota()
	p := New("test", "test@example.com", api, Options{})
	changes, cursor, err := p.Sync(context.Background(), cursorV2Prefix+"10")
	if err != nil {
		t.Fatal(err)
	}
	if cursor != cursorV2Prefix+"12" || len(changes.Upserted) != 2 {
		t.Fatalf("cursor=%s deltas=%d", cursor, len(changes.Upserted))
	}
	want := []string{"/gmail/v1/users/me/history", "/gmail/v1/users/me/threads/a", "/gmail/v1/users/me/threads/b", "/gmail/v1/users/me/threads/b"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("requests=%v", paths)
	}
}

func TestSendIsPacedButNeverRetried(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":429}}`))
	}))
	defer s.Close()
	api, err := newRealAPI(s.Client())
	if err != nil {
		t.Fatal(err)
	}
	api.svc.BasePath = s.URL + "/"
	api.quota, _ = fakeQuota()
	if err := api.SendMessage(context.Background(), "", []byte("test")); err == nil {
		t.Fatal("missing error")
	}
	if calls != 1 {
		t.Fatalf("send attempts %d", calls)
	}
}
