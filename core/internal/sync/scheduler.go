package sync

import (
	"context"
	"time"

	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/ent/thread"
	"github.com/arqueon/dankmail/core/internal/bus"
)

// Scheduler wakes snoozed threads. It holds no in-memory timers: every
// tick it queries Thread.snoozed_until <= now, so state survives daemon
// restarts by construction (spec §5).
type Scheduler struct {
	db    *ent.Client
	bus   *bus.Bus
	queue *Queue

	interval time.Duration
	// markUnread: also flag woken threads unread (configurable).
	markUnread bool
	now        nowFunc
}

func NewScheduler(db *ent.Client, b *bus.Bus, q *Queue, markUnread bool) *Scheduler {
	return &Scheduler{
		db: db, bus: b, queue: q,
		interval: 30 * time.Second, markUnread: markUnread, now: time.Now,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	tick := time.NewTicker(s.interval)
	defer tick.Stop()
	for {
		if err := s.WakeDue(ctx); err != nil && ctx.Err() == nil {
			// Logged by the caller via error return on shutdown only;
			// transient DB errors just wait for the next tick.
			_ = err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// WakeDue enqueues a snooze_wake for every thread whose snooze expired.
// Enqueue's optimistic apply clears snoozed_until, so a thread is never
// picked twice.
func (s *Scheduler) WakeDue(ctx context.Context) error {
	due, err := s.db.Thread.Query().
		Where(
			thread.SnoozedUntilNotNil(),
			thread.SnoozedUntilLTE(s.now().UTC()),
		).
		WithAccount().
		All(ctx)
	if err != nil {
		return err
	}
	for _, t := range due {
		acct := t.Edges.Account
		if acct == nil {
			continue
		}
		until := time.Time{}
		if t.SnoozedUntil != nil {
			until = *t.SnoozedUntil
		}
		err := s.queue.Enqueue(ctx, Op{
			AccountID: acct.ID,
			Type:      OpSnoozeWake,
			ThreadIDs: []string{t.ProviderThreadID},
			Payload: OpPayload{Snooze: &SnoozePayload{
				Until:      until,
				MarkUnread: s.markUnread,
			}},
		})
		if err != nil {
			return err
		}
		s.bus.Publish("snooze.woke", map[string]any{
			"accountId": acct.ID.String(),
			"threadId":  t.ProviderThreadID,
			"subject":   t.Subject,
		})
	}
	return nil
}
