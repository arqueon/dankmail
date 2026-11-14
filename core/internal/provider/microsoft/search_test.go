package microsoft

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type searchFakeAPI struct {
	*fakeAPI
	pages map[string]struct {
		ids  []string
		next string
	}
	queries []string
}

func (s *searchFakeAPI) SearchMessages(_ context.Context, query, link string, _ int) ([]string, string, error) {
	s.queries = append(s.queries, query+"|"+link)
	p := s.pages[link]
	return p.ids, p.next, nil
}

func TestSearchRemoteDeduplicatesAndBackfills(t *testing.T) {
	base := &fakeAPI{
		email:   "me@outlook.com",
		folders: testFolders,
		convs: map[string][]*graphMessage{
			"conv-1": {msg("m1", "conv-1", "fid-archive", true, false, 1700000100)},
			"conv-2": {msg("m2", "conv-2", "fid-inbox", false, true, 1700000200)},
		},
	}
	api := &searchFakeAPI{
		fakeAPI: base,
		pages: map[string]struct {
			ids  []string
			next string
		}{
			"":       {ids: []string{"conv-1", "conv-1"}, next: "page-2"},
			"page-2": {ids: []string{"conv-1", "conv-2"}, next: ""},
		},
	}

	p := New("acct-1", "me@outlook.com", api, Options{})
	changes, err := p.SearchRemote(context.Background(), "invoice 2025", 10)
	if err != nil {
		t.Fatalf("SearchRemote failed: %v", err)
	}
	if !changes.Backfill {
		t.Fatal("expected Backfill=true on SearchRemote result")
	}
	if len(changes.Upserted) != 2 {
		t.Fatalf("got %d upserted conversations, want 2", len(changes.Upserted))
	}
	if changes.Upserted[0].ThreadID != "conv-1" || changes.Upserted[1].ThreadID != "conv-2" {
		t.Fatalf("unexpected thread order: %+v", changes.Upserted)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestClientSearchMessagesSetsConsistencyHeader(t *testing.T) {
	var gotHeader, gotQuery string
	hc := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotHeader = r.Header.Get("ConsistencyLevel")
			gotQuery = r.URL.Query().Get("$search")
			body := `{"value":[{"id":"m1","conversationId":"c1"},{"id":"m2","conversationId":"c1"},{"id":"m3","conversationId":"c2"}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	c := NewClient(hc)
	ids, next, err := c.SearchMessages(context.Background(), `from:alice "budget"`, "", 25)
	if err != nil {
		t.Fatalf("SearchMessages failed: %v", err)
	}
	if gotHeader != "eventual" {
		t.Fatalf("ConsistencyLevel = %q, want eventual", gotHeader)
	}
	if gotQuery != `"from:alice \"budget\""` {
		t.Fatalf("$search = %q, want escaped query", gotQuery)
	}
	if next != "" || len(ids) != 2 || ids[0] != "c1" || ids[1] != "c2" {
		t.Fatalf("got ids=%v next=%q", ids, next)
	}
}
