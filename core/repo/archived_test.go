package repo

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/ent"
)

func TestArchivedFilter(t *testing.T) {
	ctx := context.Background()
	c := searchDB(t)
	a := c.Account.Create().SetType("gmail").SetEmail("archive@example.org").SaveX(ctx)
	b := c.Account.Create().SetType("microsoft").SetEmail("other@example.org").SaveX(ctx)
	add := func(key string, at int64, inbox bool, labels []string, snoozed bool, acct *ent.Account) *ent.Thread {
		th := addSearchThread(t, c, acct, key, "matching "+key, "", "", "")
		u := c.Thread.UpdateOne(th).SetLastMessageAt(time.Unix(at, 0)).SetInInbox(inbox).SetLabels(labels)
		if snoozed {
			u.SetSnoozedUntil(time.Now().Add(time.Hour))
		}
		return u.SaveX(ctx)
	}
	oldest := add("old", 10, false, []string{}, false, a)
	newest := add("new", 30, false, []string{"STARRED"}, false, a)
	tie := add("tie", 30, false, []string{"SENT"}, false, a)
	other := add("other", 20, false, []string{}, false, b)
	add("inbox", 60, true, []string{"INBOX"}, false, a)
	add("spam", 60, false, []string{"SPAM"}, false, a)
	add("trash", 60, false, []string{"TRASH", "custom"}, false, a)
	add("draft", 60, false, []string{"DRAFT"}, false, a)
	add("snoozed", 60, false, []string{}, true, a)
	c.Thread.UpdateOne(newest).SetUnread(true).SetStarred(true).SaveX(ctx)
	cases := []struct {
		name   string
		filter ThreadFilter
		want   []int
	}{
		{"newest first with stable ties", ThreadFilter{ArchivedOnly: true}, []int{tie.ID, newest.ID, other.ID, oldest.ID}},
		{"account", ThreadFilter{ArchivedOnly: true, AccountID: &a.ID}, []int{tie.ID, newest.ID, oldest.ID}},
		{"page", ThreadFilter{ArchivedOnly: true, Limit: 2, Offset: 1}, []int{newest.ID, other.ID}},
		{"search remains archived", ThreadFilter{ArchivedOnly: true, Query: "matching"}, []int{tie.ID, newest.ID, other.ID, oldest.ID}},
		{"unread starred", ThreadFilter{ArchivedOnly: true, UnreadOnly: true, Starred: true}, []int{newest.ID}},
		{"inbox is disjoint", ThreadFilter{ArchivedOnly: true, InboxOnly: true}, []int{}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := New(c).ListThreads(ctx, tt.filter)
			if err != nil {
				t.Fatal(err)
			}
			if got := resultIDs(rows); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
	c.Thread.UpdateOne(oldest).SetInInbox(true).SaveX(ctx)
	rows, err := New(c).ListThreads(ctx, ThreadFilter{ArchivedOnly: true, Query: "matching old"})
	if err != nil || len(rows) != 0 {
		t.Fatalf("restored thread remains archived: %v, %v", rows, err)
	}
}
