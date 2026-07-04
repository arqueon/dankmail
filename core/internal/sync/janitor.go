package sync

import (
	"context"
	"log/slog"
	"time"

	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/ent/pendingop"
	"github.com/arqueon/dankmail/core/ent/thread"
)

// Janitor is the retention/cleanup job (spec §4): prune threads older
// than the retention window — except starred or snoozed ones — and sweep
// finished PendingOps so done/failed records don't accumulate forever.
type Janitor struct {
	db            *ent.Client
	retentionDays int

	interval     time.Duration
	doneMaxAge   time.Duration
	failedMaxAge time.Duration
	now          nowFunc
}

func NewJanitor(db *ent.Client, retentionDays int) *Janitor {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	return &Janitor{
		db:            db,
		retentionDays: retentionDays,
		interval:      time.Hour,
		doneMaxAge:    24 * time.Hour,
		failedMaxAge:  7 * 24 * time.Hour, // keep failures visible a while
		now:           time.Now,
	}
}

// Run sweeps at startup and then hourly until ctx ends.
func (j *Janitor) Run(ctx context.Context) error {
	tick := time.NewTicker(j.interval)
	defer tick.Stop()
	for {
		if err := j.SweepOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("janitor sweep failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// SweepOnce applies both policies. Threads holding pending/inflight ops
// are never pruned (same freeze rule as the reconciler).
func (j *Janitor) SweepOnce(ctx context.Context) error {
	now := j.now().UTC()

	// 1. Finished ops.
	if _, err := j.db.PendingOp.Delete().
		Where(
			pendingop.StateEQ(pendingop.StateDone),
			pendingop.CreatedAtLT(now.Add(-j.doneMaxAge)),
		).
		Exec(ctx); err != nil {
		return err
	}
	if _, err := j.db.PendingOp.Delete().
		Where(
			pendingop.StateEQ(pendingop.StateFailed),
			pendingop.CreatedAtLT(now.Add(-j.failedMaxAge)),
		).
		Exec(ctx); err != nil {
		return err
	}

	// 2. Thread retention: drop conversations older than the window
	// unless the user pinned them (starred) or snoozed them.
	frozen := map[string]bool{}
	rows, err := j.db.PendingOp.Query().
		Where(pendingop.StateIn(pendingop.StatePending, pendingop.StateInflight)).
		Select(pendingop.FieldProviderThreadIds).
		All(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		for _, id := range row.ProviderThreadIds {
			frozen[id] = true
		}
	}

	cutoff := now.AddDate(0, 0, -j.retentionDays)
	old, err := j.db.Thread.Query().
		Where(
			thread.LastMessageAtLT(cutoff),
			thread.StarredEQ(false),
			thread.SnoozedUntilIsNil(),
		).
		All(ctx)
	if err != nil {
		return err
	}
	pruned := 0
	for _, t := range old {
		if frozen[t.ProviderThreadID] {
			continue
		}
		if err := j.db.Thread.DeleteOne(t).Exec(ctx); err != nil {
			return err
		}
		pruned++
	}
	if pruned > 0 {
		slog.Info("janitor pruned threads beyond retention", "count", pruned, "retentionDays", j.retentionDays)
	}
	return nil
}
