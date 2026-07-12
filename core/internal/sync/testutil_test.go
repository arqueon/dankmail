package sync

import (
	"context"
	"database/sql"
	"fmt"
	gosync "sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/internal/bus"
	"github.com/arqueon/dankmail/core/internal/provider"
	"github.com/arqueon/dankmail/core/internal/rules"

	_ "modernc.org/sqlite"
)

// testDB opens a per-test in-memory SQLite (unique name so parallel tests
// never share state) with the schema migrated.
func testDB(t *testing.T) *ent.Client {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(ON)", uuid.NewString())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the shared in-memory DB alive for the whole test.
	db.SetMaxIdleConns(1)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func mkAccount(t *testing.T, db *ent.Client) *ent.Account {
	t.Helper()
	a, err := db.Account.Create().
		SetType("gmail").
		SetEmail("ruben@example.org").
		Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

type threadOpts struct {
	id           string
	unread       bool
	starred      bool
	inInbox      bool
	snoozedUntil *time.Time
	lastMessage  time.Time
	messageCount int
}

func mkThread(t *testing.T, db *ent.Client, acct *ent.Account, o threadOpts) *ent.Thread {
	t.Helper()
	if o.lastMessage.IsZero() {
		o.lastMessage = time.Unix(1000, 0).UTC()
	}
	if o.messageCount == 0 {
		o.messageCount = 1
	}
	c := db.Thread.Create().
		SetAccount(acct).
		SetProviderThreadID(o.id).
		SetSubject("s:" + o.id).
		SetLastMessageAt(o.lastMessage).
		SetUnread(o.unread).
		SetStarred(o.starred).
		SetInInbox(o.inInbox).
		SetMessageCount(o.messageCount)
	if o.snoozedUntil != nil {
		c.SetSnoozedUntil(*o.snoozedUntil)
	}
	th, err := c.Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// fakeProvider records calls and returns programmable errors.
type flagCall struct {
	ids         []string
	add, remove []provider.Flag
}

type fakeProvider struct {
	mu         gosync.Mutex
	accountID  string
	errs       map[string]error // method name → error to return
	flagCalls  []flagCall
	archived   [][]string
	unarchived [][]string
	trashed    [][]string
	unspammed  [][]string
	spammed    [][]string
	replies    []provider.ReplyDraft
	composed   []provider.ComposeDraft
}

func newFakeProvider(accountID string) *fakeProvider {
	return &fakeProvider{accountID: accountID, errs: map[string]error{}}
}

func (f *fakeProvider) fail(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[method] = err
}

func (f *fakeProvider) errFor(method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.errs[method]
}

func (f *fakeProvider) ID() string { return f.accountID }
func (f *fakeProvider) Capabilities() provider.Capability {
	return provider.CapModifyFlags | provider.CapArchive | provider.CapTrash |
		provider.CapSendReply | provider.CapCompose | provider.CapHistorySync
}

func (f *fakeProvider) Sync(ctx context.Context, cursor string) (provider.Changes, string, error) {
	return provider.Changes{}, cursor, f.errFor("Sync")
}

func (f *fakeProvider) ModifyFlags(ctx context.Context, ids []string, add, remove []provider.Flag) error {
	if err := f.errFor("ModifyFlags"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flagCalls = append(f.flagCalls, flagCall{ids: ids, add: add, remove: remove})
	return nil
}

func (f *fakeProvider) Archive(ctx context.Context, ids []string) error {
	if err := f.errFor("Archive"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.archived = append(f.archived, ids)
	return nil
}

func (f *fakeProvider) Unarchive(ctx context.Context, ids []string) error {
	if err := f.errFor("Unarchive"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unarchived = append(f.unarchived, ids)
	return nil
}

func (f *fakeProvider) Trash(ctx context.Context, ids []string) error {
	if err := f.errFor("Trash"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trashed = append(f.trashed, ids)
	return nil
}

func (f *fakeProvider) Unspam(ctx context.Context, ids []string) error {
	if err := f.errFor("Unspam"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unspammed = append(f.unspammed, ids)
	return nil
}

func (f *fakeProvider) Spam(ctx context.Context, ids []string) error {
	if err := f.errFor("Spam"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spammed = append(f.spammed, ids)
	return nil
}

func (f *fakeProvider) SendReply(ctx context.Context, threadID string, r provider.ReplyDraft) error {
	if err := f.errFor("SendReply"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, r)
	return nil
}

func (f *fakeProvider) Compose(ctx context.Context, m provider.ComposeDraft) error {
	if err := f.errFor("Compose"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.composed = append(f.composed, m)
	return nil
}

func (f *fakeProvider) WebLink(threadID, messageID string) (string, bool) { return "", false }

// regMap is a static Registry for tests.
type regMap map[uuid.UUID]provider.Provider

func (r regMap) Provider(id uuid.UUID) (provider.Provider, bool) {
	p, ok := r[id]
	return p, ok
}

// rig bundles the wired-up components most tests need.
type rig struct {
	db    *ent.Client
	bus   *bus.Bus
	queue *Queue
	acct  *ent.Account
	prov  *fakeProvider
	exec  *Executor
	now   time.Time
}

func newRig(t *testing.T, policies rules.Policies) *rig {
	t.Helper()
	db := testDB(t)
	b := bus.New()
	acct := mkAccount(t, db)
	prov := newFakeProvider(acct.ID.String())
	q := NewQueue(db, b, func() rules.Policies { return policies })
	reg := regMap{acct.ID: prov}
	exec := NewExecutor(db, b, q, reg, acct.ID)

	r := &rig{db: db, bus: b, queue: q, acct: acct, prov: prov, exec: exec,
		now: time.Unix(50_000, 0).UTC()}
	exec.now = func() time.Time { return r.now }
	return r
}

func (r *rig) advance(d time.Duration) { r.now = r.now.Add(d) }

func (r *rig) reloadThread(t *testing.T, id string) *ent.Thread {
	t.Helper()
	rows, err := r.db.Thread.Query().All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ProviderThreadID == id {
			return row
		}
	}
	t.Fatalf("thread %s not found", id)
	return nil
}

func (r *rig) ops(t *testing.T) []*ent.PendingOp {
	t.Helper()
	rows, err := r.db.PendingOp.Query().All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// collect drains up to n events from a bus subscription without blocking
// longer than the deadline.
func collect(ch <-chan bus.Event, n int, deadline time.Duration) []bus.Event {
	var out []bus.Event
	timer := time.After(deadline)
	for len(out) < n {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-timer:
			return out
		}
	}
	return out
}
