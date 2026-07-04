package sync

import (
	"context"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/ent/pendingop"
	"github.com/arqueon/dankmail/core/internal/rules"
)

func TestSchedulerWakesDueSnoozeOnce(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	past := r.now.Add(-time.Minute)
	future := r.now.Add(time.Hour)
	mkThread(t, r.db, r.acct, threadOpts{id: "due", inInbox: false, snoozedUntil: &past})
	mkThread(t, r.db, r.acct, threadOpts{id: "later", inInbox: false, snoozedUntil: &future})

	s := NewScheduler(r.db, r.bus, r.queue, true)
	s.now = func() time.Time { return r.now }
	ctx := context.Background()

	if err := s.WakeDue(ctx); err != nil {
		t.Fatal(err)
	}

	due := r.reloadThread(t, "due")
	if !due.InInbox || due.SnoozedUntil != nil || !due.Unread {
		t.Errorf("due thread = inInbox:%v snoozed:%v unread:%v, want woken+unread", due.InInbox, due.SnoozedUntil, due.Unread)
	}
	later := r.reloadThread(t, "later")
	if later.InInbox || later.SnoozedUntil == nil {
		t.Error("future snooze must not wake")
	}

	ops := r.ops(t)
	if len(ops) != 1 || ops[0].OpType != pendingop.OpTypeSnoozeWake {
		t.Fatalf("ops = %+v, want exactly one snooze_wake", ops)
	}

	// Second pass: the woken thread no longer matches — no duplicates.
	if err := s.WakeDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(r.ops(t)); got != 1 {
		t.Errorf("ops after second pass = %d, want still 1", got)
	}
}

func TestSchedulerSurvivesRestartByConstruction(t *testing.T) {
	// State lives in Thread.snoozed_until: a brand-new Scheduler over the
	// same DB picks up pending wakes with no in-memory handoff.
	r := newRig(t, rules.DefaultPolicies())
	past := r.now.Add(-time.Second)
	mkThread(t, r.db, r.acct, threadOpts{id: "due", inInbox: false, snoozedUntil: &past})

	fresh := NewScheduler(r.db, r.bus, r.queue, false)
	fresh.now = func() time.Time { return r.now }
	if err := fresh.WakeDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if th := r.reloadThread(t, "due"); !th.InInbox {
		t.Error("a fresh scheduler over the same DB must wake due snoozes")
	}
}
