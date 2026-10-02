package microsoft

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/errdefs"
)

type recordingAPI struct {
	*fakeAPI
	reads          []string
	fail           string
	folderFailures int
	deltaCalls     int
}

func (f *recordingAPI) ListConversation(ctx context.Context, id string) ([]*graphMessage, error) {
	f.reads = append(f.reads, id)
	if id == f.fail {
		return nil, &graphError{Status: 429, Until: time.Now().Add(time.Minute)}
	}
	return f.fakeAPI.ListConversation(ctx, id)
}
func (f *recordingAPI) FolderIDs(ctx context.Context) (map[string]string, error) {
	if f.folderFailures > 0 {
		f.folderFailures--
		return nil, errors.New("temporary network failure")
	}
	return f.fakeAPI.FolderIDs(ctx)
}
func (f *recordingAPI) DeltaMessages(ctx context.Context, folder, link string) (deltaPage, error) {
	f.deltaCalls++
	return f.fakeAPI.DeltaMessages(ctx, folder, link)
}
func TestCheckpointResumesConversationsAndFolderFailureCanRecover(t *testing.T) {
	ctx := context.Background()
	f := &recordingAPI{fakeAPI: &fakeAPI{folders: testFolders, deltas: map[string][]deltaPage{folderInbox: {{DeltaLink: "delta-inbox", Messages: []*graphMessage{{ConversationID: "c1"}, {ConversationID: "c2"}}}}}, convs: map[string][]*graphMessage{"c1": {msg("m1", "c1", "fid-inbox", false, false, 1)}, "c2": {msg("m2", "c2", "fid-inbox", false, false, 2)}}}, fail: "c2"}
	p := New("one", "me@outlook.com", f, Options{})
	ch, cursor, err := p.Sync(ctx, "")
	if errdefs.KindOf(err) != errdefs.KindRateLimit || !ch.Incomplete || len(ch.Upserted) != 1 || errdefs.RetryAfter(err) <= 0 {
		t.Fatalf("partial batch lost: %+v %v", ch, err)
	}
	priorDeltaCalls := f.deltaCalls
	f.fail = ""
	f.folderFailures = 1
	p = New("one", "me@outlook.com", f, Options{})
	_, cursor, err = p.Sync(ctx, cursor)
	if err == nil {
		t.Fatal("wanted temporary folder failure")
	}
	ch, cursor, err = p.Sync(ctx, cursor)
	if err != nil || ch.Incomplete || len(ch.Upserted) != 1 || len(ch.SnapshotThreadIDs) != 2 || strings.HasPrefix(cursor, checkpointPrefix) {
		t.Fatalf("resume failed: %+v %v", ch, err)
	}
	if f.deltaCalls != priorDeltaCalls || strings.Join(f.reads, ",") != "c1,c2,c2,c2" {
		t.Fatalf("replayed work: %v delta=%d", f.reads, f.deltaCalls)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGraphRetryAfterAndContinuationOrigin(t *testing.T) {
	for _, header := range []string{"120", time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat), ""} {
		t.Run(header, func(t *testing.T) {
			calls := 0
			c := NewClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if !strings.Contains(r.Header.Get("Prefer"), "ImmutableId") {
					t.Error("missing stable message IDs")
				}
				return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{header}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"TooManyRequests"}}`))}, nil
			})})
			_, err := c.GetProfile(context.Background())
			if errdefs.RetryAfter(classify(err)) < 50*time.Second {
				t.Fatal("lost Retry-After", err)
			}
			_, err = c.GetProfile(context.Background())
			if err == nil || calls != 1 {
				t.Fatal("sent request during cooldown", calls, err)
			}
		})
	}
	calls := 0
	c := NewClient(&http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected request") })})
	for _, u := range []string{"http://graph.microsoft.com/v1.0/me", "https://evil.example/delta", "https://graph.microsoft.com@evil.example/delta"} {
		if err := c.do(context.Background(), http.MethodGet, u, nil, nil); err == nil {
			t.Fatal("accepted foreign origin")
		}
	}
	if calls != 0 {
		t.Fatal("sent authorization to untrusted origin")
	}
}

type sentAPI struct {
	*recordingAPI
	pages int
}

func (f *sentAPI) ListSentMessages(ctx context.Context, link string) ([]string, string, error) {
	f.pages++
	return []string{"c1", "c2"}, "", nil
}
func TestSentPageResumeAndSentFlags(t *testing.T) {
	f := &sentAPI{recordingAPI: &recordingAPI{fakeAPI: &fakeAPI{folders: map[string]string{folderSent: "sent", folderInbox: "inbox"}, convs: map[string][]*graphMessage{"c1": {msg("m1", "c1", "sent", true, false, 1)}, "c2": {msg("m2", "c2", "sent", true, false, 2)}}}, fail: "c2"}}
	p := New("one", "me@outlook.com", f, Options{})
	ch, next, err := p.SentPage(context.Background(), "")
	if err == nil || len(ch.Upserted) != 1 || !ch.Backfill || !ch.Upserted[0].Messages[0].IsSent {
		t.Fatal(ch, err)
	}
	f.fail = ""
	ch, next, err = p.SentPage(context.Background(), next)
	if err != nil || next != "" || len(ch.Upserted) != 1 || f.pages != 1 {
		t.Fatal(ch, next, err, f.pages)
	}
}
func TestSentClientOrdersAndBoundsGraphPage(t *testing.T) {
	c := NewClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1.0/me/mailFolders/sentitems/messages" || r.URL.Query().Get("$orderby") != "sentDateTime desc" || r.URL.Query().Get("$top") != "25" {
			t.Error("wrong sent query", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":[{"conversationId":"c1"},{"conversationId":"c1"},{"conversationId":"c2"}],"@odata.nextLink":"https://graph.microsoft.com/next"}`))}, nil
	})})
	ids, next, err := c.ListSentMessages(context.Background(), "")
	if err != nil || strings.Join(ids, ",") != "c1,c2" || next != "https://graph.microsoft.com/next" {
		t.Fatal(ids, next, err)
	}
}
