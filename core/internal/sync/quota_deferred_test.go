package sync

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/ent/pendingop"
	"github.com/arqueon/dankmail/core/errdefs"
	"github.com/arqueon/dankmail/core/internal/bus"
	"github.com/arqueon/dankmail/core/internal/rules"
)

type retryHint struct{ delay time.Duration }

func (e *retryHint) Error() string             { return "retry later" }
func (e *retryHint) RetryAfter() time.Duration { return e.delay }

func TestDeferredSyncSchedulesRetryAndVisitsOtherAccounts(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprintf("full=%t", full), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			db := testDB(t)
			limited := mkAccount(t, db)
			healthy, err := db.Account.Create().SetType("gmail").SetEmail("healthy@example.com").Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
			p := newFakeProvider(limited.ID.String())
			hint := &retryHint{time.Hour}
			p.fail("Sync", fmt.Errorf("gmail: %w", errdefs.Wrap(errdefs.KindRateLimit, hint)))
			e := NewEngine(db, bus.New(), nil, regMap{
				limited.ID: p, healthy.ID: newFakeProvider(healthy.ID.String()),
			}, nil, nil)
			err = e.SyncAll(ctx, full)
			if !errors.Is(err, hint) || ctx.Err() != nil {
				t.Fatalf("lost deferral or blocked: %v", err)
			}
			if got := e.nextSyncDelay(limited, err); got != time.Hour {
				t.Fatalf("retry scheduled in %s, want 1h", got)
			}
			if got := e.nextSyncDelay(healthy, nil); got != DefaultPollInterval {
				t.Fatalf("healthy cadence changed: %s", got)
			}
			stored, err := db.Account.Get(ctx, healthy.ID)
			if err != nil || stored.LastSyncAt.IsZero() || stored.LastError != "" {
				t.Fatalf("healthy account was not synced: %+v %v", stored, err)
			}
			stored, err = db.Account.Get(ctx, limited.ID)
			if err != nil || stored.Status != account.StatusActive || stored.NeedsReauth {
				t.Fatalf("quota must not require reauthentication: %+v %v", stored, err)
			}
		})
	}
}

func TestExecutorSchedulesProviderRetryAfter(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: true})
	ctx := context.Background()
	r.prov.fail("Archive", fmt.Errorf("gmail: %w", errdefs.Wrap(errdefs.KindRateLimit, &retryHint{time.Hour})))
	if err := r.queue.Enqueue(ctx, Op{AccountID: r.acct.ID, Type: OpArchive, ThreadIDs: []string{"t1"}}); err != nil {
		t.Fatal(err)
	}
	r.exec.Drain(ctx)
	op := r.ops(t)[0]
	if op.State != pendingop.StatePending || op.NextAttemptAt == nil || !op.NextAttemptAt.Equal(r.now.Add(time.Hour)) {
		t.Fatalf("provider delay not honored: %+v", op)
	}
	r.advance(30 * time.Minute)
	r.exec.Drain(ctx)
	if got := r.ops(t)[0].Attempts; got != 1 {
		t.Fatalf("retried before cooldown elapsed: attempts=%d", got)
	}
	r.advance(30 * time.Minute)
	r.prov.fail("Archive", nil)
	r.exec.Drain(ctx)
	if got := r.ops(t)[0].State; got != pendingop.StateDone {
		t.Fatalf("retry did not resume: %s", got)
	}
}
