package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/ent/message"
	"github.com/arqueon/dankmail/core/ent/thread"
	"github.com/arqueon/dankmail/core/models"
)

func searchDB(t testing.TB) *ent.Client {
	t.Helper()
	c, err := OpenFile(context.Background(), filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func addSearchThread(t testing.TB, c *ent.Client, account *ent.Account, key, subject, snippet, from, body string) *ent.Thread {
	t.Helper()
	ctx := context.Background()
	th, err := c.Thread.Create().SetAccount(account).SetProviderThreadID(key).
		SetSubject(subject).SetSnippet(snippet).SetLastMessageAt(time.Unix(1000, 0)).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Message.Create().SetThread(th).SetProviderMessageID(key).
		SetFrom(from).SetBodyText(body).SetDate(time.Unix(1000, 0)).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func resultIDs(rows []models.ThreadSummary) []int {
	out := make([]int, len(rows))
	for i, row := range rows {
		out[i] = row.ID
	}
	return out
}

func TestSearchMatchesLiteralLegacySearch(t *testing.T) {
	ctx := context.Background()
	c := searchDB(t)
	a := c.Account.Create().SetType("gmail").SetEmail("test@example.org").SaveX(ctx)
	addSearchThread(t, c, a, "subject", "Invoice ALPHA 100% paid", "", "", "")
	addSearchThread(t, c, a, "snippet", "", `file_a C:\drafts café 東京大学`, "", "")
	addSearchThread(t, c, a, "sender", "", "", "Ada Example <ada@example.org>", "")
	addSearchThread(t, c, a, "body", "", "", "", `Budget OR "design" notes; O'Reilly, 1000 paid, fileXa`)
	for _, query := range []string{"ALPHA", "lph", "a", "ad", "no such term", "100%", "%", "_", "file_a", `C:\drafts`, "café", "東京大", "ada@", `OR "design"`, "O'Reilly"} {
		t.Run(query, func(t *testing.T) {
			got, err := New(c).ListThreads(ctx, ThreadFilter{Query: query, Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			want := c.Thread.Query().Where(thread.Or(thread.SubjectContainsFold(query), thread.SnippetContainsFold(query),
				thread.HasMessagesWith(message.Or(message.FromContainsFold(query), message.BodyTextContainsFold(query))))).
				Order(ent.Desc(thread.FieldLastMessageAt), ent.Desc(thread.FieldID)).AllX(ctx)
			ids := make([]int, len(want))
			for i, th := range want {
				ids[i] = th.ID
			}
			if !reflect.DeepEqual(resultIDs(got), ids) {
				t.Fatalf("query %q: got %v, want %v", query, resultIDs(got), ids)
			}
		})
	}
}

func TestSearchMigrationBackfillAndTriggers(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mail.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	legacy := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	if err := legacy.Schema.Create(ctx); err != nil {
		t.Fatal(err)
	}
	a := legacy.Account.Create().SetType("gmail").SetEmail("test@example.org").SaveX(ctx)
	th := addSearchThread(t, legacy, a, "old", "original subject", "original snippet", "original sender", "original body")
	legacy.Close()
	for pass := 0; pass < 2; pass++ {
		c, err := OpenFile(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := New(c).ListThreads(ctx, ThreadFilter{Query: "original"})
		if err != nil || len(rows) != 1 {
			t.Fatalf("open %d: rows=%v err=%v", pass, resultIDs(rows), err)
		}
		c.Close()
	}
	c, err := OpenFile(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := New(c)
	assertMatches := func(query string, n int) {
		t.Helper()
		rows, err := r.ListThreads(ctx, ThreadFilter{Query: query})
		if err != nil || len(rows) != n {
			t.Fatalf("%q: got %d, want %d, err=%v", query, len(rows), n, err)
		}
	}
	c.Thread.UpdateOneID(th.ID).SetSubject("changed subject").SetSnippet("changed snippet").SaveX(ctx)
	c.Message.Update().Where(message.HasThreadWith(thread.IDEQ(th.ID))).SetFrom("changed sender").SetBodyText("changed body").SaveX(ctx)
	assertMatches("original", 0)
	assertMatches("changed", 1)
	tx, err := c.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Thread.UpdateOneID(th.ID).SetSubject("rolled back").SaveX(ctx)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertMatches("rolled back", 0)
	assertMatches("changed subject", 1)
	c.Thread.DeleteOneID(th.ID).ExecX(ctx)
	assertMatches("changed", 0)
	addSearchThread(t, c, a, "new", "new subject", "", "", "new body")
	assertMatches("new body", 1)
	c.Account.DeleteOneID(a.ID).ExecX(ctx)
	assertMatches("new body", 0)
}

func TestSearchFiltersAndPagination(t *testing.T) {
	ctx := context.Background()
	c := searchDB(t)
	r := New(c)
	a := c.Account.Create().SetType("gmail").SetEmail("first@example.org").SaveX(ctx)
	b := c.Account.Create().SetType("gmail").SetEmail("second@example.org").SaveX(ctx)
	for i := 0; i < 205; i++ {
		addSearchThread(t, c, a, fmt.Sprint(i), "matching subject", "", "", "")
	}
	outside := addSearchThread(t, c, b, "snoozed", "matching subject", "", "", "")
	c.Thread.UpdateOneID(outside.ID).SetUnread(true).SetStarred(true).SetInInbox(false).
		SetLabels([]string{"SPAM"}).SetSnoozedUntil(time.Now().Add(time.Hour)).SaveX(ctx)
	rows, err := r.ListThreads(ctx, ThreadFilter{Query: "matching", AccountID: &b.ID, UnreadOnly: true, Starred: true, Label: "SPAM", ExcludeInbox: true})
	if err != nil || len(rows) != 1 || rows[0].ID != outside.ID {
		t.Fatalf("filtered search: %v %v", rows, err)
	}
	rows, err = r.ListThreads(ctx, ThreadFilter{AccountID: &b.ID})
	if err != nil || len(rows) != 0 {
		t.Fatalf("snoozed inbox: %v %v", rows, err)
	}
	first, err := r.ListThreads(ctx, ThreadFilter{Query: "matching", AccountID: &a.ID, Limit: 201})
	if err != nil || len(first) != 201 {
		t.Fatalf("first page: %d %v", len(first), err)
	}
	last, err := r.ListThreads(ctx, ThreadFilter{Query: "matching", AccountID: &a.ID, Limit: 201, Offset: 200})
	if err != nil || len(last) != 5 {
		t.Fatalf("last page: %d %v", len(last), err)
	}
	if first[200].ID != last[0].ID {
		t.Fatal("unstable ordering for equal timestamps")
	}
	seen := map[int]bool{}
	for _, row := range append(first[:200], last...) {
		if seen[row.ID] {
			t.Fatal("duplicate page entry")
		}
		seen[row.ID] = true
	}
	if len(seen) != 205 {
		t.Fatal("pagination lost results")
	}
}

func BenchmarkThreadSearch(b *testing.B) {
	ctx := context.Background()
	c := searchDB(b)
	r := New(c)
	a := c.Account.Create().SetType("gmail").SetEmail("benchmark@example.org").SaveX(ctx)
	for i := 0; i < 10000; i++ {
		body := strings.Repeat("ordinary cached message text ", 140)
		if i%1000 == 0 {
			body += " unique-search-needle"
		}
		addSearchThread(b, c, a, fmt.Sprint(i), "routine subject", "", "sender@example.org", body)
	}
	b.Run("indexed", func(b *testing.B) {
		for b.Loop() {
			if _, err := r.ListThreads(ctx, ThreadFilter{Query: "unique-search-needle", Limit: 200}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("legacy", func(b *testing.B) {
		for b.Loop() {
			bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err := c.Thread.Query().WithAccount().Where(thread.Or(thread.SubjectContainsFold("unique-search-needle"), thread.SnippetContainsFold("unique-search-needle"), thread.HasMessagesWith(message.Or(message.FromContainsFold("unique-search-needle"), message.BodyTextContainsFold("unique-search-needle"))))).Order(ent.Desc(thread.FieldLastMessageAt), ent.Desc(thread.FieldID)).Limit(200).All(bounded)
			cancel()
			if errors.Is(err, context.DeadlineExceeded) {
				b.Skip("legacy query exceeded the 5-second per-search budget")
			}
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
