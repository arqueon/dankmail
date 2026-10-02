package gmail

import (
	"context"
	"fmt"
	"strings"
	"testing"

	gmailv1 "google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

type recordingSyncAPI struct {
	*fakeAPI
	fetched []string
	pages   []string
}

func (f *recordingSyncAPI) GetThread(ctx context.Context, id string) (*gmailv1.Thread, error) {
	f.fetched = append(f.fetched, id)
	return f.fakeAPI.GetThread(ctx, id)
}
func (f *recordingSyncAPI) ListHistory(ctx context.Context, start uint64, page string) (*gmailv1.ListHistoryResponse, error) {
	f.pages = append(f.pages, page)
	return f.fakeAPI.ListHistory(ctx, start, page)
}
func TestFullSyncCheckpointSurvivesProviderRestart(t *testing.T) {
	ctx := context.Background()
	f := &recordingSyncAPI{fakeAPI: &fakeAPI{profileHist: 10, threads: fixtureThreads(), threadErr: map[string]error{"t2": &googleapi.Error{Code: 429}}, listPages: map[string][]listPage{"INBOX": {{ids: []string{"t1", "t2"}}}, "SPAM": {{ids: []string{"t1", "t3"}}}}}}
	p := New("acct-1", "me@example.org", f, Options{})
	ch, cursor, err := p.Sync(ctx, "")
	if err == nil || !ch.Incomplete || !ch.FullResync || len(ch.Upserted) != 1 || !strings.HasPrefix(cursor, syncCheckpointPrefix) {
		t.Fatalf("lost first batch: %+v, %v", ch, err)
	}
	if strings.Contains(cursor, "body") {
		t.Fatal("body leaked into checkpoint")
	}
	delete(f.threadErr, "t2")
	p = New("acct-1", "me@example.org", f, Options{})
	ch, cursor, err = p.Sync(ctx, cursor)
	if err != nil || ch.Incomplete || cursor != "labels-v2:10" || len(ch.SnapshotThreadIDs) != 3 {
		t.Fatalf("resume failed: %+v, %s, %v", ch, cursor, err)
	}
	counts := map[string]int{}
	for _, id := range f.fetched {
		counts[id]++
	}
	if counts["t1"] != 1 || counts["t2"] != 2 || counts["t3"] != 1 {
		t.Fatalf("replayed completed work: %v", counts)
	}
}
func TestIncrementalBatchesResumeWithoutReplayingHistory(t *testing.T) {
	ctx := context.Background()
	f := &recordingSyncAPI{fakeAPI: &fakeAPI{threads: map[string]*gmailv1.Thread{}}}
	history := &gmailv1.History{Id: 20}
	for i := 0; i < 60; i++ {
		id := fmt.Sprintf("t%02d", i)
		item := *fixtureThreads()["t1"]
		item.Id = id
		f.threads[id] = &item
		history.MessagesAdded = append(history.MessagesAdded, &gmailv1.HistoryMessageAdded{Message: &gmailv1.Message{ThreadId: id}})
	}
	f.histPages = []*gmailv1.ListHistoryResponse{{HistoryId: 20, History: []*gmailv1.History{history}}}
	cursor := "labels-v2:10"
	count := 0
	batches := 0
	for {
		p := New("acct-1", "me@example.org", f, Options{})
		ch, next, err := p.Sync(ctx, cursor)
		if err != nil {
			t.Fatal(err)
		}
		count += len(ch.Upserted)
		batches++
		cursor = next
		if !ch.Incomplete {
			break
		}
		if batches > 5 {
			t.Fatal("never completed")
		}
	}
	if count != 60 || batches < 3 || cursor != "labels-v2:20" || len(f.pages) != 1 || len(f.fetched) != 60 {
		t.Fatalf("count=%d batches=%d history=%v fetches=%d", count, batches, f.pages, len(f.fetched))
	}
}

func TestSentPageIsBoundedAndKeepsFailedSuffix(t *testing.T) {
	f := &fakeAPI{threads: fixtureThreads(), searchPages: []listPage{{ids: []string{"t1", "t2"}, next: "1"}, {ids: []string{"t3"}}}, threadErr: map[string]error{"t2": &googleapi.Error{Code: 429}}}
	p := newTestProvider(f, Options{})
	first, cursor, err := p.SentPage(context.Background(), "")
	if err == nil || len(first.Upserted) != 1 || !first.Backfill || f.lastQuery != "in:sent -in:trash -is:draft" || f.lastPageSize != 25 {
		t.Fatalf("unexpected sent page: %+v %v", first, err)
	}
	if _, _, err := p.ArchivedPage(context.Background(), cursor); err == nil {
		t.Fatal("sent cursor accepted by archive")
	}
	delete(f.threadErr, "t2")
	rest, next, err := p.SentPage(context.Background(), cursor)
	if err != nil || len(rest.Upserted) != 1 || rest.Upserted[0].ThreadID != "t2" || next == "" {
		t.Fatal(rest, next, err)
	}
	last, next, err := p.SentPage(context.Background(), next)
	if err != nil || len(last.Upserted) != 1 || last.Upserted[0].ThreadID != "t3" || next != "" {
		t.Fatal(last, next, err)
	}
}
