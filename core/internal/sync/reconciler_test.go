package sync

import (
	"context"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/ent/pendingop"
	"github.com/arqueon/dankmail/core/internal/bus"
	"github.com/arqueon/dankmail/core/internal/provider"
	"github.com/arqueon/dankmail/core/internal/rules"
)

func delta(id string, opts func(*provider.ThreadDelta)) provider.ThreadDelta {
	d := provider.ThreadDelta{
		ThreadID:     id,
		Subject:      "s:" + id,
		LastMessage:  1000,
		Unread:       false,
		Starred:      false,
		InInbox:      true,
		MessageCount: 1,
	}
	if opts != nil {
		opts(&d)
	}
	return d
}

func TestReconcilerRemoteWinsOnCleanThread(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", unread: true, inInbox: true})
	rec := NewReconciler(r.db, r.bus)

	err := rec.Apply(context.Background(), r.acct.ID, provider.Changes{
		Upserted: []provider.ThreadDelta{delta("t1", func(d *provider.ThreadDelta) {
			d.Unread = false
			d.InInbox = false
			d.Starred = true
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	th := r.reloadThread(t, "t1")
	if th.Unread || th.InInbox || !th.Starred {
		t.Errorf("state = unread:%v inInbox:%v starred:%v, want remote state applied", th.Unread, th.InInbox, th.Starred)
	}
}

func TestReconcilerFreezesThreadsWithPendingOps(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", unread: true, inInbox: true})
	ctx := context.Background()

	// User marks read → optimistic + pending op.
	if err := r.queue.Enqueue(ctx, Op{AccountID: r.acct.ID, Type: OpMarkRead, ThreadIDs: []string{"t1"}}); err != nil {
		t.Fatal(err)
	}
	rec := NewReconciler(r.db, r.bus)

	// A stale sync arrives claiming the thread is still unread: frozen.
	stale := provider.Changes{Upserted: []provider.ThreadDelta{delta("t1", func(d *provider.ThreadDelta) {
		d.Unread = true
	})}}
	if err := rec.Apply(ctx, r.acct.ID, stale); err != nil {
		t.Fatal(err)
	}
	if th := r.reloadThread(t, "t1"); th.Unread {
		t.Fatal("frozen thread must keep the optimistic read state")
	}

	// Op resolves → the freeze lifts and remote wins again.
	if _, err := r.db.PendingOp.Update().SetState(pendingop.StateDone).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if err := rec.Apply(ctx, r.acct.ID, stale); err != nil {
		t.Fatal(err)
	}
	if th := r.reloadThread(t, "t1"); !th.Unread {
		t.Error("after the op resolves, remote state must overwrite local")
	}
}

func TestReconcilerFrozenThreadStillReceivesMessages(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", unread: false, inInbox: true})
	ctx := context.Background()

	if err := r.queue.Enqueue(ctx, Op{AccountID: r.acct.ID, Type: OpArchive, ThreadIDs: []string{"t1"}}); err != nil {
		t.Fatal(err)
	}
	rec := NewReconciler(r.db, r.bus)
	ch := provider.Changes{Upserted: []provider.ThreadDelta{delta("t1", func(d *provider.ThreadDelta) {
		d.Messages = []provider.MessageDelta{{MessageID: "m9", From: "ada@example.com", Date: 2000}}
	})}}
	if err := rec.Apply(ctx, r.acct.ID, ch); err != nil {
		t.Fatal(err)
	}

	n, err := r.db.Message.Query().Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("messages = %d, want 1 (frozen thread still ingests messages)", n)
	}
	if th := r.reloadThread(t, "t1"); th.InInbox {
		t.Error("frozen thread state (archived) must not be overwritten")
	}
}

func TestReconcilerSnoozeCancelledByNewMessage(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	until := time.Unix(90_000, 0).UTC()
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: false, snoozedUntil: &until, lastMessage: time.Unix(1000, 0)})
	rec := NewReconciler(r.db, r.bus)

	ch := provider.Changes{Upserted: []provider.ThreadDelta{delta("t1", func(d *provider.ThreadDelta) {
		d.LastMessage = 5000 // newer message arrived
		d.MessageCount = 2
		d.InInbox = true
	})}}
	if err := rec.Apply(context.Background(), r.acct.ID, ch); err != nil {
		t.Fatal(err)
	}
	th := r.reloadThread(t, "t1")
	if th.SnoozedUntil != nil || !th.InInbox {
		t.Errorf("state = snoozed:%v inInbox:%v, want snooze cancelled and re-shown", th.SnoozedUntil, th.InInbox)
	}
}

func TestReconcilerSnoozeSurvivesOwnArchiveEcho(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	until := time.Unix(90_000, 0).UTC()
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: false, snoozedUntil: &until, lastMessage: time.Unix(1000, 0)})
	rec := NewReconciler(r.db, r.bus)

	// The echo of our own snooze-archive: same content, out of inbox.
	ch := provider.Changes{Upserted: []provider.ThreadDelta{delta("t1", func(d *provider.ThreadDelta) {
		d.InInbox = false
	})}}
	if err := rec.Apply(context.Background(), r.acct.ID, ch); err != nil {
		t.Fatal(err)
	}
	th := r.reloadThread(t, "t1")
	if th.SnoozedUntil == nil || th.InInbox {
		t.Errorf("state = snoozed:%v inInbox:%v, want snooze kept", th.SnoozedUntil, th.InInbox)
	}
}

func TestReconcilerFullResyncPrunesMissingButKeepsSnoozed(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "keep", inInbox: true})
	mkThread(t, r.db, r.acct, threadOpts{id: "gone", inInbox: true})
	until := time.Unix(90_000, 0).UTC()
	mkThread(t, r.db, r.acct, threadOpts{id: "snoozed", inInbox: false, snoozedUntil: &until})
	rec := NewReconciler(r.db, r.bus)

	ch := provider.Changes{
		FullResync: true,
		Upserted:   []provider.ThreadDelta{delta("keep", nil)},
	}
	if err := rec.Apply(context.Background(), r.acct.ID, ch); err != nil {
		t.Fatal(err)
	}

	rows, err := r.db.Thread.Query().All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, row := range rows {
		got[row.ProviderThreadID] = true
	}
	if !got["keep"] || !got["snoozed"] || got["gone"] {
		t.Errorf("surviving threads = %v, want keep+snoozed, gone pruned", got)
	}
}

func TestReconcilerRemovedThreadDeleted(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: true})
	rec := NewReconciler(r.db, r.bus)

	if err := rec.Apply(context.Background(), r.acct.ID, provider.Changes{
		RemovedThreadIDs: []string{"t1"},
	}); err != nil {
		t.Fatal(err)
	}
	n, err := r.db.Thread.Query().Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("threads = %d, want 0", n)
	}
}

func TestReconcilerArrivalEventsOnlyOnIncrementalSync(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	rec := NewReconciler(r.db, r.bus)
	ctx := context.Background()

	mk := func(msgID string, full bool) []bus.Event {
		_, events := r.bus.Subscribe(32)
		ch := provider.Changes{
			FullResync: full,
			Upserted: []provider.ThreadDelta{delta("t-"+msgID, func(d *provider.ThreadDelta) {
				d.Messages = []provider.MessageDelta{{MessageID: msgID, From: "ada@example.com", Date: 2000}}
			})},
		}
		if err := rec.Apply(ctx, r.acct.ID, ch); err != nil {
			t.Fatal(err)
		}
		return collect(events, 8, 100*time.Millisecond)
	}

	arrived := func(evs []bus.Event) int {
		n := 0
		for _, ev := range evs {
			if ev.Topic == "message.arrived" {
				n++
			}
		}
		return n
	}

	if n := arrived(mk("m1", true)); n != 0 {
		t.Errorf("FullResync fired %d message.arrived events, want 0", n)
	}
	if n := arrived(mk("m2", false)); n != 1 {
		t.Errorf("incremental sync fired %d message.arrived events, want 1", n)
	}
}
