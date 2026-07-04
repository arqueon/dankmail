// Package sync owns the two loops at the heart of the daemon:
//
//   - the sync engine: per-account polling (or push wake-up) that pulls
//     remote Changes through the Provider interface and reconciles them
//     into the local cache;
//   - the pending-op queue executor: drains user actions (PendingOp rows)
//     against the provider with retries and exponential backoff.
//
// Reconciliation rule (the one invariant everything depends on): the
// remote is the authority and sync overwrites local thread state —
// EXCEPT for threads that have a PendingOp in state pending/inflight,
// whose local state is frozen until the op finishes (done or failed).
// On permanent failure the op's payload carries the pre-op state and the
// executor reverts the optimistic local change, then notifies the user.
package sync

import (
	"context"
	gosync "sync"
	"time"

	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/errdefs"
	"github.com/arqueon/dankmail/core/internal/bus"
)

// DefaultPollInterval between provider syncs, overridable per account via
// Account.config["pollSeconds"].
const DefaultPollInterval = 60 * time.Second

// Engine wires, per active account, a poll loop (provider.Sync →
// Reconciler.Apply → cursor persistence) and an op-queue Executor.
type Engine struct {
	db         *ent.Client
	bus        *bus.Bus
	queue      *Queue
	registry   Registry
	reconciler *Reconciler
	scheduler  *Scheduler
}

func NewEngine(db *ent.Client, b *bus.Bus, q *Queue, reg Registry, sched *Scheduler) *Engine {
	return &Engine{
		db: db, bus: b, queue: q, registry: reg,
		reconciler: NewReconciler(db, b), scheduler: sched,
	}
}

// Run starts loops for every account present at call time and blocks
// until ctx cancels. Account add/remove restarts the engine (the daemon
// cancels and re-runs; loops are stateless between runs).
func (e *Engine) Run(ctx context.Context) error {
	accts, err := e.db.Account.Query().All(ctx)
	if err != nil {
		return err
	}

	var wg gosync.WaitGroup
	for _, a := range accts {
		wg.Add(1)
		go func(a *ent.Account) {
			defer wg.Done()
			e.runAccount(ctx, a)
		}(a)

		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			_ = NewExecutor(e.db, e.bus, e.queue, e.registry, id).Run(ctx)
		}(a.ID)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = e.scheduler.Run(ctx)
	}()

	wg.Wait()
	return nil
}

// runAccount is the poll loop for one account.
func (e *Engine) runAccount(ctx context.Context, a *ent.Account) {
	interval := DefaultPollInterval
	if secs, ok := a.Config["pollSeconds"].(float64); ok && secs >= 10 {
		interval = time.Duration(secs) * time.Second
	}
	// First sync immediately, then on the ticker.
	_ = e.SyncAccount(ctx, a.ID)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			_ = e.SyncAccount(ctx, a.ID)
		}
	}
}

// SyncAccount performs one provider sync + reconcile pass. Also invoked
// by the IPC "system.sync" method (dmail sync).
func (e *Engine) SyncAccount(ctx context.Context, accountID uuid.UUID) error {
	acct, err := e.db.Account.Get(ctx, accountID)
	if err != nil {
		return err
	}
	if acct.Status != account.StatusActive {
		return nil
	}
	prov, ok := e.registry.Provider(accountID)
	if !ok {
		return errNoProvider
	}

	changes, cursor, err := prov.Sync(ctx, acct.SyncCursor)
	if err != nil {
		upd := e.db.Account.UpdateOneID(accountID).SetLastError(err.Error())
		if errdefs.KindOf(err) == errdefs.KindAuth {
			upd.SetStatus(account.StatusAuthError)
			e.bus.Publish("account.auth", map[string]any{"accountId": accountID.String()})
		}
		_, _ = upd.Save(ctx)
		return err
	}

	if err := e.reconciler.Apply(ctx, accountID, changes); err != nil {
		return err
	}
	_, err = e.db.Account.UpdateOneID(accountID).
		SetSyncCursor(cursor).
		SetLastSyncAt(time.Now()).
		SetLastError("").
		Save(ctx)
	return err
}
