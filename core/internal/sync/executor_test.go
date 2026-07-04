package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/ent/pendingop"
	"github.com/arqueon/dankmail/core/errdefs"
	"github.com/arqueon/dankmail/core/internal/provider"
	"github.com/arqueon/dankmail/core/internal/rules"
)

func TestExecutorAppliesMarkReadAndCompletes(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", unread: true, inInbox: true})
	ctx := context.Background()

	if err := r.queue.Enqueue(ctx, Op{AccountID: r.acct.ID, Type: OpMarkRead, ThreadIDs: []string{"t1"}}); err != nil {
		t.Fatal(err)
	}
	r.exec.Drain(ctx)

	if len(r.prov.flagCalls) != 1 {
		t.Fatalf("flag calls = %d, want 1", len(r.prov.flagCalls))
	}
	fc := r.prov.flagCalls[0]
	if len(fc.remove) != 1 || fc.remove[0] != provider.FlagUnread || len(fc.add) != 0 {
		t.Errorf("call = add:%v remove:%v, want remove unread", fc.add, fc.remove)
	}
	if ops := r.ops(t); ops[0].State != pendingop.StateDone {
		t.Errorf("op state = %s, want done", ops[0].State)
	}
}

func TestExecutorCoalescesHomogeneousBatch(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: true})
	mkThread(t, r.db, r.acct, threadOpts{id: "t2", inInbox: true})
	ctx := context.Background()

	for _, id := range []string{"t1", "t2"} {
		if err := r.queue.Enqueue(ctx, Op{AccountID: r.acct.ID, Type: OpArchive, ThreadIDs: []string{id}}); err != nil {
			t.Fatal(err)
		}
	}
	r.exec.Drain(ctx)

	if len(r.prov.archived) != 1 {
		t.Fatalf("Archive calls = %d, want 1 coalesced", len(r.prov.archived))
	}
	if got := r.prov.archived[0]; len(got) != 2 {
		t.Errorf("coalesced ids = %v, want both threads", got)
	}
}

func TestExecutorRetriesThenFailsAndReverts(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: true})
	r.prov.fail("Archive", errdefs.Wrap(errdefs.KindNetwork, errors.New("net down")))
	ctx := context.Background()

	if err := r.queue.Enqueue(ctx, Op{AccountID: r.acct.ID, Type: OpArchive, ThreadIDs: []string{"t1"}}); err != nil {
		t.Fatal(err)
	}
	if th := r.reloadThread(t, "t1"); th.InInbox {
		t.Fatal("optimistic archive should hide from inbox")
	}

	// Attempt 1..5: each drain consumes one attempt, then waits backoff.
	for i := 0; i < MaxAttempts; i++ {
		r.exec.Drain(ctx)
		r.advance(10 * time.Minute) // beyond any backoff step
	}

	ops := r.ops(t)
	if ops[0].State != pendingop.StateFailed {
		t.Fatalf("op state = %s (attempts=%d), want failed", ops[0].State, ops[0].Attempts)
	}
	if th := r.reloadThread(t, "t1"); !th.InInbox {
		t.Error("failed op must revert the optimistic archive")
	}
}

func TestExecutorPermanentErrorFailsImmediately(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", unread: true, inInbox: true})
	r.prov.fail("Trash", errdefs.Wrap(errdefs.KindPermanent, errors.New("gone")))
	ctx := context.Background()

	_, events := r.bus.Subscribe(32)
	if err := r.queue.Enqueue(ctx, Op{AccountID: r.acct.ID, Type: OpTrash, ThreadIDs: []string{"t1"}}); err != nil {
		t.Fatal(err)
	}
	r.exec.Drain(ctx)

	var trashOp bool
	for _, op := range r.ops(t) {
		if op.OpType == pendingop.OpTypeTrash {
			trashOp = true
			if op.State != pendingop.StateFailed {
				t.Errorf("trash op state = %s, want failed on first attempt", op.State)
			}
		}
	}
	if !trashOp {
		t.Fatal("trash op missing")
	}
	// The revert restores the pre-trash snapshot (back to inbox).
	if th := r.reloadThread(t, "t1"); !th.InInbox {
		t.Error("permanent failure must revert the optimistic trash")
	}
	var sawFailed bool
	for _, ev := range collect(events, 8, 100*time.Millisecond) {
		if ev.Topic == "op.failed" {
			sawFailed = true
		}
	}
	if !sawFailed {
		t.Error("expected op.failed event on the bus")
	}
}

func TestExecutorAuthErrorPausesAccountAndKeepsOps(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: true})
	r.prov.fail("Archive", errdefs.Wrap(errdefs.KindAuth, errors.New("token revoked")))
	ctx := context.Background()

	if err := r.queue.Enqueue(ctx, Op{AccountID: r.acct.ID, Type: OpArchive, ThreadIDs: []string{"t1"}}); err != nil {
		t.Fatal(err)
	}
	r.exec.Drain(ctx)

	acct, err := r.db.Account.Get(ctx, r.acct.ID)
	if err != nil {
		t.Fatal(err)
	}
	if acct.Status != account.StatusAuthError {
		t.Errorf("account status = %s, want auth_error", acct.Status)
	}
	ops := r.ops(t)
	if ops[0].State != pendingop.StatePending || ops[0].Attempts != 0 {
		t.Errorf("op = state:%s attempts:%d, want pending/0 (waits for re-auth)", ops[0].State, ops[0].Attempts)
	}
	// Optimistic state stays (not reverted) while waiting for re-auth.
	if th := r.reloadThread(t, "t1"); th.InInbox {
		t.Error("optimistic archive must persist while account is paused")
	}
	// Paused account: nothing processes even when due.
	r.prov.fail("Archive", nil)
	r.exec.Drain(ctx)
	if got := r.ops(t)[0].State; got != pendingop.StatePending {
		t.Errorf("op state after drain on paused account = %s, want still pending", got)
	}
}

func TestExecutorSnoozeWakeUnarchivesAndMarksUnread(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	until := r.now.Add(-time.Minute)
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: false, snoozedUntil: &until})
	ctx := context.Background()

	if err := r.queue.Enqueue(ctx, Op{
		AccountID: r.acct.ID, Type: OpSnoozeWake, ThreadIDs: []string{"t1"},
		Payload: OpPayload{Snooze: &SnoozePayload{Until: until, MarkUnread: true}},
	}); err != nil {
		t.Fatal(err)
	}
	r.exec.Drain(ctx)

	if len(r.prov.unarchived) != 1 {
		t.Fatalf("Unarchive calls = %d, want 1", len(r.prov.unarchived))
	}
	if len(r.prov.flagCalls) != 1 || r.prov.flagCalls[0].add[0] != provider.FlagUnread {
		t.Errorf("expected follow-up add-unread call, got %+v", r.prov.flagCalls)
	}
	th := r.reloadThread(t, "t1")
	if !th.InInbox || th.SnoozedUntil != nil || !th.Unread {
		t.Errorf("state = %+v, want inInbox+unread, no snooze", th)
	}
}
