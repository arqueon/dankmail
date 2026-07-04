package sync

import (
	"context"
	"encoding/json"
	"fmt"
	gosync "sync"
	"time"

	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/ent/pendingop"
	"github.com/arqueon/dankmail/core/ent/thread"
	"github.com/arqueon/dankmail/core/internal/bus"
	"github.com/arqueon/dankmail/core/internal/rules"
)

// Queue is the single write path for user actions. Enqueue expands the op
// through the action hooks, snapshots the pre-op thread state, applies the
// optimistic local change, and inserts the PendingOp rows — all in one
// transaction — then wakes the account's executor.
type Queue struct {
	db       *ent.Client
	bus      *bus.Bus
	policies rules.Policies

	mu    gosync.Mutex
	wakes map[uuid.UUID]chan struct{}
}

func NewQueue(db *ent.Client, b *bus.Bus, policies rules.Policies) *Queue {
	return &Queue{db: db, bus: b, policies: policies, wakes: map[uuid.UUID]chan struct{}{}}
}

// WakeChan returns the (buffered, size-1) channel the account's executor
// selects on. Created on first use.
func (q *Queue) WakeChan(accountID uuid.UUID) <-chan struct{} {
	q.mu.Lock()
	defer q.mu.Unlock()
	ch, ok := q.wakes[accountID]
	if !ok {
		ch = make(chan struct{}, 1)
		q.wakes[accountID] = ch
	}
	return ch
}

// Wake nudges the executor without blocking.
func (q *Queue) Wake(accountID uuid.UUID) {
	q.mu.Lock()
	ch, ok := q.wakes[accountID]
	if !ok {
		ch = make(chan struct{}, 1)
		q.wakes[accountID] = ch
	}
	q.mu.Unlock()
	select {
	case ch <- struct{}{}:
	default:
	}
}

// Enqueue queues op (plus its hook expansions) for the account.
func (q *Queue) Enqueue(ctx context.Context, op Op) error {
	ops := q.expand(op)

	tx, err := q.db.Tx(ctx)
	if err != nil {
		return err
	}
	for i := range ops {
		if err := enqueueOne(ctx, tx, &ops[i]); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	q.Wake(op.AccountID)
	q.bus.Publish("threads.changed", map[string]any{"accountId": op.AccountID.String()})
	return nil
}

// expand applies the chained-action policies (spec §5). Hook ops precede
// the primary op so a coalesced batch executes them first.
func (q *Queue) expand(op Op) []Op {
	var trigger rules.Trigger
	switch op.Type {
	case OpTrash:
		trigger = rules.TriggerTrash
	case OpStar:
		trigger = rules.TriggerStar
	case OpSendReply:
		trigger = rules.TriggerReplySent
	default:
		return []Op{op}
	}
	extra := q.policies.Expand(trigger)
	ops := make([]Op, 0, len(extra)+1)
	for _, t := range extra {
		ops = append(ops, Op{
			AccountID: op.AccountID,
			Type:      OpType(t),
			ThreadIDs: append([]string(nil), op.ThreadIDs...),
		})
	}
	return append(ops, op)
}

func enqueueOne(ctx context.Context, tx *ent.Tx, op *Op) error {
	// Snapshot pre-op state for revert, then apply optimistically.
	rows, err := opThreads(ctx, tx, op.AccountID, op.ThreadIDs)
	if err != nil {
		return err
	}
	if op.Payload.Prev == nil {
		op.Payload.Prev = map[string]ThreadState{}
	}
	for _, t := range rows {
		op.Payload.Prev[t.ProviderThreadID] = snapshotState(t)
	}
	if err := applyLocal(ctx, tx, op, rows); err != nil {
		return err
	}

	payload, err := payloadToMap(op.Payload)
	if err != nil {
		return err
	}
	row, err := tx.PendingOp.Create().
		SetAccountID(op.AccountID).
		SetOpType(pendingop.OpType(op.Type)).
		SetProviderThreadIds(op.ThreadIDs).
		SetPayload(payload).
		Save(ctx)
	if err != nil {
		return err
	}
	op.ID = row.ID
	return nil
}

// opThreads loads the local thread rows an op touches.
func opThreads(ctx context.Context, tx *ent.Tx, accountID uuid.UUID, threadIDs []string) ([]*ent.Thread, error) {
	if len(threadIDs) == 0 {
		return nil, nil
	}
	return tx.Thread.Query().
		Where(
			thread.HasAccountWith(account.IDEQ(accountID)),
			thread.ProviderThreadIDIn(threadIDs...),
		).
		All(ctx)
}

func snapshotState(t *ent.Thread) ThreadState {
	return ThreadState{
		Unread:       t.Unread,
		Starred:      t.Starred,
		InInbox:      t.InInbox,
		SnoozedUntil: t.SnoozedUntil,
	}
}

// applyLocal performs the optimistic local mutation for op.
func applyLocal(ctx context.Context, tx *ent.Tx, op *Op, rows []*ent.Thread) error {
	for _, t := range rows {
		u := tx.Thread.UpdateOne(t)
		switch op.Type {
		case OpMarkRead:
			u.SetUnread(false)
		case OpMarkUnread:
			u.SetUnread(true)
		case OpStar:
			u.SetStarred(true)
		case OpUnstar:
			u.SetStarred(false)
		case OpArchive:
			u.SetInInbox(false)
		case OpUnarchive:
			u.SetInInbox(true)
		case OpTrash:
			u.SetInInbox(false)
		case OpSnooze:
			if op.Payload.Snooze == nil {
				return fmt.Errorf("sync: snooze op without payload")
			}
			u.SetInInbox(false).SetSnoozedUntil(op.Payload.Snooze.Until)
		case OpSnoozeWake:
			u.SetInInbox(true).ClearSnoozedUntil()
			if op.Payload.Snooze != nil && op.Payload.Snooze.MarkUnread {
				u.SetUnread(true)
			}
		case OpSendReply, OpCompose:
			continue // no local thread state change
		default:
			return fmt.Errorf("sync: unknown op type %q", op.Type)
		}
		if _, err := u.Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

// revertLocal restores the pre-op snapshots after a permanent failure.
func revertLocal(ctx context.Context, tx *ent.Tx, op Op) error {
	if len(op.Payload.Prev) == 0 {
		return nil
	}
	rows, err := opThreads(ctx, tx, op.AccountID, op.ThreadIDs)
	if err != nil {
		return err
	}
	for _, t := range rows {
		prev, ok := op.Payload.Prev[t.ProviderThreadID]
		if !ok {
			continue
		}
		u := tx.Thread.UpdateOne(t).
			SetUnread(prev.Unread).
			SetStarred(prev.Starred).
			SetInInbox(prev.InInbox)
		if prev.SnoozedUntil != nil {
			u.SetSnoozedUntil(*prev.SnoozedUntil)
		} else {
			u.ClearSnoozedUntil()
		}
		if _, err := u.Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

func payloadToMap(p OpPayload) (map[string]any, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func payloadFromMap(m map[string]any) (OpPayload, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return OpPayload{}, err
	}
	var p OpPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return OpPayload{}, err
	}
	return p, nil
}

func opFromRow(row *ent.PendingOp, accountID uuid.UUID) (Op, error) {
	payload, err := payloadFromMap(row.Payload)
	if err != nil {
		return Op{}, err
	}
	return Op{
		ID:        row.ID,
		AccountID: accountID,
		Type:      OpType(row.OpType),
		ThreadIDs: row.ProviderThreadIds,
		Payload:   payload,
		Attempts:  row.Attempts,
	}, nil
}

// withTx runs fn in a transaction with rollback on error/panic.
func withTx(ctx context.Context, db *ent.Client, fn func(tx *ent.Tx) error) error {
	tx, err := db.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()
	if err := fn(tx); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			err = fmt.Errorf("%w (rollback: %v)", err, rerr)
		}
		return err
	}
	return tx.Commit()
}

// nowFunc is the clock seam for tests.
type nowFunc func() time.Time
