package sync

import (
	"context"

	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/ent/message"
	"github.com/arqueon/dankmail/core/ent/pendingop"
	"github.com/arqueon/dankmail/core/ent/thread"
	"github.com/arqueon/dankmail/core/internal/bus"
	"github.com/arqueon/dankmail/core/internal/provider"
)

// Reconciler applies provider.Changes to the local cache.
//
// The one invariant (spec §5): the remote is the authority and local
// thread state is overwritten — EXCEPT threads holding a pending/inflight
// op, which are frozen (messages still land; thread-level state does not
// move) until the op resolves.
type Reconciler struct {
	db  *ent.Client
	bus *bus.Bus
}

func NewReconciler(db *ent.Client, b *bus.Bus) *Reconciler {
	return &Reconciler{db: db, bus: b}
}

// Apply ingests one Sync result for the account. It publishes
// threads.changed once, and message.arrived for every genuinely new
// message that should be considered for notification.
func (r *Reconciler) Apply(ctx context.Context, accountID uuid.UUID, ch provider.Changes) error {
	frozen, err := r.frozenThreadIDs(ctx, accountID)
	if err != nil {
		return err
	}

	var arrivals []map[string]any
	err = withTx(ctx, r.db, func(tx *ent.Tx) error {
		seen := make(map[string]bool, len(ch.Upserted))
		for _, delta := range ch.Upserted {
			seen[delta.ThreadID] = true
			// A FullResync is a backfill: it must never fire
			// notifications for the history it (re)ingests.
			arr, err := upsertThread(ctx, tx, accountID, delta, frozen[delta.ThreadID], !ch.FullResync)
			if err != nil {
				return err
			}
			arrivals = append(arrivals, arr...)
		}

		for _, id := range ch.RemovedThreadIDs {
			if frozen[id] {
				continue
			}
			if _, err := tx.Thread.Delete().
				Where(
					thread.HasAccountWith(account.IDEQ(accountID)),
					thread.ProviderThreadIDEQ(id),
				).
				Exec(ctx); err != nil {
				return err
			}
		}

		if ch.FullResync {
			// The payload is a complete snapshot: anything local that is
			// not in it is gone remotely — except frozen threads (op in
			// flight) and snoozed ones (locally archived on purpose).
			stale, err := tx.Thread.Query().
				Where(
					thread.HasAccountWith(account.IDEQ(accountID)),
					thread.SnoozedUntilIsNil(),
				).
				All(ctx)
			if err != nil {
				return err
			}
			for _, t := range stale {
				if seen[t.ProviderThreadID] || frozen[t.ProviderThreadID] {
					continue
				}
				if err := tx.Thread.DeleteOne(t).Exec(ctx); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	r.bus.Publish("threads.changed", map[string]any{"accountId": accountID.String()})
	for _, a := range arrivals {
		r.bus.Publish("message.arrived", a)
	}
	return nil
}

func (r *Reconciler) frozenThreadIDs(ctx context.Context, accountID uuid.UUID) (map[string]bool, error) {
	rows, err := r.db.PendingOp.Query().
		Where(
			pendingop.HasAccountWith(account.IDEQ(accountID)),
			pendingop.StateIn(pendingop.StatePending, pendingop.StateInflight),
		).
		Select(pendingop.FieldProviderThreadIds).
		All(ctx)
	if err != nil {
		return nil, err
	}
	frozen := map[string]bool{}
	for _, row := range rows {
		for _, id := range row.ProviderThreadIds {
			frozen[id] = true
		}
	}
	return frozen, nil
}

// upsertThread reconciles one delta and returns message.arrived payloads
// for new messages (only when notify is true). Frozen threads only
// receive messages; their thread-level state does not move.
func upsertThread(ctx context.Context, tx *ent.Tx, accountID uuid.UUID, d provider.ThreadDelta, isFrozen, notify bool) ([]map[string]any, error) {
	existing, err := tx.Thread.Query().
		Where(
			thread.HasAccountWith(account.IDEQ(accountID)),
			thread.ProviderThreadIDEQ(d.ThreadID),
		).
		Only(ctx)
	switch {
	case ent.IsNotFound(err):
		existing = nil
	case err != nil:
		return nil, err
	}

	var row *ent.Thread
	isNew := existing == nil
	if isNew {
		row, err = tx.Thread.Create().
			SetAccountID(accountID).
			SetProviderThreadID(d.ThreadID).
			SetSubject(d.Subject).
			SetSnippet(d.Snippet).
			SetLastMessageAt(timeFromUnix(d.LastMessage)).
			SetParticipants(d.Participants).
			SetUnread(d.Unread).
			SetStarred(d.Starred).
			SetInInbox(d.InInbox).
			SetLabels(d.Labels).
			SetMessageCount(d.MessageCount).
			Save(ctx)
		if err != nil {
			return nil, err
		}
	} else if isFrozen {
		row = existing
	} else {
		u := tx.Thread.UpdateOne(existing).
			SetSubject(d.Subject).
			SetSnippet(d.Snippet).
			SetLastMessageAt(timeFromUnix(d.LastMessage)).
			SetParticipants(d.Participants).
			SetUnread(d.Unread).
			SetStarred(d.Starred).
			SetInInbox(d.InInbox).
			SetLabels(d.Labels).
			SetMessageCount(d.MessageCount)

		// Snooze cancellation (spec §5): a real remote change on a
		// snoozed thread wakes it immediately. The echo of our own
		// snooze-archive (inInbox=false, no new content, same flags)
		// must NOT cancel; hence the three "real change" signals.
		if existing.SnoozedUntil != nil {
			newContent := timeFromUnix(d.LastMessage).After(existing.LastMessageAt) ||
				d.MessageCount > existing.MessageCount
			flagsChanged := d.Unread != existing.Unread || d.Starred != existing.Starred
			reInboxed := d.InInbox && !existing.InInbox
			if newContent || flagsChanged || reInboxed {
				u.ClearSnoozedUntil()
			} else {
				// Keep the snooze; the thread stays locally archived.
				u.SetInInbox(false)
			}
		}
		row, err = u.Save(ctx)
		if err != nil {
			return nil, err
		}
	}

	var arrivals []map[string]any
	for _, m := range d.Messages {
		exists, err := tx.Message.Query().
			Where(
				message.HasThreadWith(thread.IDEQ(row.ID)),
				message.ProviderMessageIDEQ(m.MessageID),
			).
			Exist(ctx)
		if err != nil {
			return nil, err
		}
		if exists {
			continue
		}
		if _, err := tx.Message.Create().
			SetThreadID(row.ID).
			SetProviderMessageID(m.MessageID).
			SetRfc822MessageID(m.RFC822MessageID).
			SetFrom(m.From).
			SetTo(m.To).
			SetCc(m.Cc).
			SetDate(timeFromUnix(m.Date)).
			SetSnippet(m.Snippet).
			SetBodyText(m.BodyText).
			SetReplyHeaders(replyHeaders(m)).
			Save(ctx); err != nil {
			return nil, err
		}
		if notify {
			arrivals = append(arrivals, map[string]any{
				"accountId": accountID.String(),
				"threadId":  d.ThreadID,
				"messageId": m.MessageID,
				"from":      m.From,
				"subject":   d.Subject,
				"snippet":   m.Snippet,
				"inInbox":   d.InInbox,
			})
		}
	}
	return arrivals, nil
}
