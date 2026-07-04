package sync

import (
	"context"
	"testing"

	"github.com/arqueon/dankmail/core/ent/pendingop"
	"github.com/arqueon/dankmail/core/internal/rules"
)

func TestEnqueueAppliesOptimisticallyAndSnapshotsPrev(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", unread: true, inInbox: true})

	err := r.queue.Enqueue(context.Background(), Op{
		AccountID: r.acct.ID, Type: OpMarkRead, ThreadIDs: []string{"t1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	th := r.reloadThread(t, "t1")
	if th.Unread {
		t.Error("optimistic apply: thread should be read locally")
	}

	ops := r.ops(t)
	if len(ops) != 1 || ops[0].State != pendingop.StatePending {
		t.Fatalf("ops = %+v, want one pending", ops)
	}
	op, err := opFromRow(ops[0], r.acct.ID)
	if err != nil {
		t.Fatal(err)
	}
	prev, ok := op.Payload.Prev["t1"]
	if !ok || !prev.Unread {
		t.Errorf("prev snapshot = %+v, want unread=true captured", op.Payload.Prev)
	}
}

func TestEnqueueTrashHookAddsMarkRead(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", unread: true, inInbox: true})

	if err := r.queue.Enqueue(context.Background(), Op{
		AccountID: r.acct.ID, Type: OpTrash, ThreadIDs: []string{"t1"},
	}); err != nil {
		t.Fatal(err)
	}

	ops := r.ops(t)
	if len(ops) != 2 {
		t.Fatalf("got %d ops, want 2 (hook mark_read + trash)", len(ops))
	}
	if ops[0].OpType != pendingop.OpTypeMarkRead || ops[1].OpType != pendingop.OpTypeTrash {
		t.Errorf("op order = %s, %s; want mark_read then trash", ops[0].OpType, ops[1].OpType)
	}
	th := r.reloadThread(t, "t1")
	if th.Unread || th.InInbox {
		t.Errorf("optimistic state = unread:%v inInbox:%v, want read + out of inbox", th.Unread, th.InInbox)
	}
}

func TestEnqueueStarHookOffByDefault(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: false})

	if err := r.queue.Enqueue(context.Background(), Op{
		AccountID: r.acct.ID, Type: OpStar, ThreadIDs: []string{"t1"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(r.ops(t)); got != 1 {
		t.Fatalf("got %d ops, want 1 (unarchive hook default off)", got)
	}
	if th := r.reloadThread(t, "t1"); th.InInbox {
		t.Error("star must not unarchive with the hook off")
	}
}

func TestEnqueueStarHookUnarchivesWhenEnabled(t *testing.T) {
	p := rules.DefaultPolicies()
	p.UnarchiveOnStar = true
	r := newRig(t, p)
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: false})

	if err := r.queue.Enqueue(context.Background(), Op{
		AccountID: r.acct.ID, Type: OpStar, ThreadIDs: []string{"t1"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(r.ops(t)); got != 2 {
		t.Fatalf("got %d ops, want 2 (unarchive + star)", got)
	}
	th := r.reloadThread(t, "t1")
	if !th.InInbox || !th.Starred {
		t.Errorf("state = inInbox:%v starred:%v, want both true", th.InInbox, th.Starred)
	}
}

func TestEnqueueSnoozeSetsDeadline(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: true})

	until := r.now.Add(2 * 3600e9)
	if err := r.queue.Enqueue(context.Background(), Op{
		AccountID: r.acct.ID, Type: OpSnooze, ThreadIDs: []string{"t1"},
		Payload: OpPayload{Snooze: &SnoozePayload{Until: until}},
	}); err != nil {
		t.Fatal(err)
	}
	th := r.reloadThread(t, "t1")
	if th.InInbox || th.SnoozedUntil == nil || !th.SnoozedUntil.Equal(until) {
		t.Errorf("state = inInbox:%v snoozedUntil:%v, want archived until %v", th.InInbox, th.SnoozedUntil, until)
	}
}
