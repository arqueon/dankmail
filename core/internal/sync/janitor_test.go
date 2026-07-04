package sync

import (
	"context"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/ent/pendingop"
	"github.com/arqueon/dankmail/core/internal/rules"
)

func TestJanitorPrunesOldThreadsButKeepsPinned(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	old := r.now.AddDate(0, 0, -45)
	snoozeUntil := r.now.Add(time.Hour)

	mkThread(t, r.db, r.acct, threadOpts{id: "old-plain", inInbox: false, lastMessage: old})
	mkThread(t, r.db, r.acct, threadOpts{id: "old-starred", starred: true, inInbox: false, lastMessage: old})
	mkThread(t, r.db, r.acct, threadOpts{id: "old-snoozed", inInbox: false, snoozedUntil: &snoozeUntil, lastMessage: old})
	mkThread(t, r.db, r.acct, threadOpts{id: "recent", inInbox: true, lastMessage: r.now.Add(-time.Hour)})

	j := NewJanitor(r.db, 30)
	j.now = func() time.Time { return r.now }
	if err := j.SweepOnce(context.Background()); err != nil {
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
	if got["old-plain"] {
		t.Error("old-plain should be pruned")
	}
	for _, keep := range []string{"old-starred", "old-snoozed", "recent"} {
		if !got[keep] {
			t.Errorf("%s should survive retention", keep)
		}
	}
}

func TestJanitorKeepsFrozenThreadsAndSweepsFinishedOps(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	old := r.now.AddDate(0, 0, -45)
	mkThread(t, r.db, r.acct, threadOpts{id: "old-with-op", inInbox: true, lastMessage: old})
	ctx := context.Background()

	// Pending op freezes the thread against pruning.
	if err := r.queue.Enqueue(ctx, Op{AccountID: r.acct.ID, Type: OpArchive, ThreadIDs: []string{"old-with-op"}}); err != nil {
		t.Fatal(err)
	}
	// Plus one finished op old enough to sweep and one fresh failure.
	oldDone, err := r.db.PendingOp.Create().
		SetAccountID(r.acct.ID).
		SetOpType(pendingop.OpTypeMarkRead).
		SetState(pendingop.StateDone).
		SetCreatedAt(r.now.Add(-48 * time.Hour)).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	freshFailed, err := r.db.PendingOp.Create().
		SetAccountID(r.acct.ID).
		SetOpType(pendingop.OpTypeTrash).
		SetState(pendingop.StateFailed).
		SetCreatedAt(r.now.Add(-time.Hour)).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	j := NewJanitor(r.db, 30)
	j.now = func() time.Time { return r.now }
	if err := j.SweepOnce(ctx); err != nil {
		t.Fatal(err)
	}

	if n, _ := r.db.Thread.Query().Count(ctx); n != 1 {
		t.Errorf("threads = %d, want frozen thread kept", n)
	}
	if _, err := r.db.PendingOp.Get(ctx, oldDone.ID); err == nil {
		t.Error("old done op should be swept")
	}
	if _, err := r.db.PendingOp.Get(ctx, freshFailed.ID); err != nil {
		t.Error("fresh failed op should be kept for visibility")
	}
}
