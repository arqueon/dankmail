package repo

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestSentOrdersByOutgoingDateAndScopesAccount(t *testing.T) {
	ctx := context.Background()
	db := searchDB(t)
	a := db.Account.Create().SetType("gmail").SetEmail("one@example.org").SaveX(ctx)
	b := db.Account.Create().SetType("microsoft").SetEmail("two@example.org").SaveX(ctx)
	old := addSearchThread(t, db, a, "old", "Old outgoing, new reply", "", "", "body")
	recent := addSearchThread(t, db, a, "recent", "Recent outgoing", "", "", "body")
	received := addSearchThread(t, db, a, "received", "Received only", "", "", "body")
	other := addSearchThread(t, db, b, "other", "Other account", "", "", "body")
	_ = received
	db.Thread.UpdateOne(old).SetLastMessageAt(time.Unix(9999, 0)).ExecX(ctx)
	for _, entry := range []struct {
		id   int
		date int64
	}{{old.ID, 10}, {recent.ID, 30}, {other.ID, 20}} {
		db.Message.Create().SetThreadID(entry.id).SetProviderMessageID("sent").SetIsSent(true).SetDate(time.Unix(entry.date, 0)).SaveX(ctx)
	}
	r := New(db)
	rows, err := r.ListThreads(ctx, ThreadFilter{SentOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resultIDs(rows), []int{recent.ID, other.ID, old.ID}) {
		t.Fatal("not ordered by outgoing date", resultIDs(rows))
	}
	if rows[0].LastMessageAt.Unix() != 30 || rows[2].LastMessageAt.Unix() != 10 {
		t.Fatal("Sent dates show received replies")
	}
	rows, err = r.ListThreads(ctx, ThreadFilter{SentOnly: true, AccountID: &a.ID, Limit: 1, Offset: 1})
	if err != nil || !reflect.DeepEqual(resultIDs(rows), []int{old.ID}) {
		t.Fatal(rows, err)
	}
	detail, err := r.GetThread(ctx, recent.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range detail.Messages {
		found = found || m.IsSent
	}
	if !found {
		t.Fatal("reader cannot identify outgoing message")
	}
}
