package sync

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/ent/pendingop"
	"github.com/arqueon/dankmail/core/errdefs"
	"github.com/arqueon/dankmail/core/internal/bus"
	"github.com/arqueon/dankmail/core/internal/provider"
)

// MaxAttempts before a retryable op is declared failed.
const MaxAttempts = 5

// defaultBackoff delays indexed by (attempts-1). Rate-limit errors use
// the same ladder unless the provider error carries its own delay.
var defaultBackoff = []time.Duration{
	2 * time.Second, 8 * time.Second, 30 * time.Second, 2 * time.Minute, 5 * time.Minute,
}

const batchLimit = 50

// Executor drains one account's pending ops against its provider.
type Executor struct {
	db        *ent.Client
	bus       *bus.Bus
	queue     *Queue
	registry  Registry
	accountID uuid.UUID

	now     nowFunc
	backoff []time.Duration
	// pollEvery bounds how long a due retry waits when no wake arrives.
	pollEvery time.Duration
}

func NewExecutor(db *ent.Client, b *bus.Bus, q *Queue, reg Registry, accountID uuid.UUID) *Executor {
	return &Executor{
		db: db, bus: b, queue: q, registry: reg, accountID: accountID,
		now: time.Now, backoff: defaultBackoff, pollEvery: 5 * time.Second,
	}
}

// Run drains until ctx is cancelled, waking on Queue signals or the
// retry poll tick.
func (e *Executor) Run(ctx context.Context) error {
	wake := e.queue.WakeChan(e.accountID)
	tick := time.NewTicker(e.pollEvery)
	defer tick.Stop()
	for {
		e.Drain(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
		case <-tick.C:
		}
	}
}

// Drain processes batches until the due queue is empty or blocked.
func (e *Executor) Drain(ctx context.Context) {
	for {
		n, err := e.processBatch(ctx)
		if err != nil || n == 0 {
			return
		}
	}
}

// processBatch executes one coalesced batch. Returns how many ops it
// consumed (0 = nothing due).
func (e *Executor) processBatch(ctx context.Context) (int, error) {
	acct, err := e.db.Account.Get(ctx, e.accountID)
	if err != nil {
		return 0, err
	}
	if acct.Status != account.StatusActive {
		return 0, nil // paused or awaiting re-auth; ops stay queued
	}

	rows, err := e.db.PendingOp.Query().
		Where(
			pendingop.HasAccountWith(account.IDEQ(e.accountID)),
			pendingop.StateEQ(pendingop.StatePending),
			pendingop.Or(
				pendingop.NextAttemptAtIsNil(),
				pendingop.NextAttemptAtLTE(e.now().UTC()),
			),
		).
		Order(ent.Asc(pendingop.FieldID)).
		Limit(batchLimit).
		All(ctx)
	if err != nil || len(rows) == 0 {
		return 0, err
	}

	// Coalesce the longest homogeneous prefix (spec §3.2: batchModify
	// when ≥2 homogeneous ops queue up).
	batch := []*ent.PendingOp{rows[0]}
	if OpType(rows[0].OpType).batchable() {
		for _, r := range rows[1:] {
			if r.OpType != rows[0].OpType {
				break
			}
			batch = append(batch, r)
		}
	}

	ops := make([]Op, len(batch))
	ids := make([]int, len(batch))
	for i, r := range batch {
		op, err := opFromRow(r, e.accountID)
		if err != nil {
			return 0, err
		}
		ops[i], ids[i] = op, r.ID
	}

	if _, err := e.db.PendingOp.Update().
		Where(pendingop.IDIn(ids...)).
		SetState(pendingop.StateInflight).
		Save(ctx); err != nil {
		return 0, err
	}

	callErr := e.call(ctx, ops)
	if callErr == nil {
		_, err := e.db.PendingOp.Update().
			Where(pendingop.IDIn(ids...)).
			SetState(pendingop.StateDone).
			SetLastError("").
			Save(ctx)
		if err != nil {
			return 0, err
		}
		e.bus.Publish("ops.applied", map[string]any{
			"accountId": e.accountID.String(),
			"opType":    string(ops[0].Type),
			"count":     len(ops),
		})
		return len(ops), nil
	}
	return len(ops), e.handleFailure(ctx, ops, ids, callErr)
}

// call maps a homogeneous batch onto one (or two, for snooze_wake)
// provider calls. Thread IDs are merged.
func (e *Executor) call(ctx context.Context, ops []Op) error {
	prov, ok := e.registry.Provider(e.accountID)
	if !ok {
		return errdefs.Wrap(errdefs.KindPermanent, errNoProvider)
	}
	ids := mergeThreadIDs(ops)
	switch ops[0].Type {
	case OpMarkRead:
		return prov.ModifyFlags(ctx, ids, nil, []provider.Flag{provider.FlagUnread})
	case OpMarkUnread:
		return prov.ModifyFlags(ctx, ids, []provider.Flag{provider.FlagUnread}, nil)
	case OpStar:
		return prov.ModifyFlags(ctx, ids, []provider.Flag{provider.FlagStarred}, nil)
	case OpUnstar:
		return prov.ModifyFlags(ctx, ids, nil, []provider.Flag{provider.FlagStarred})
	case OpArchive, OpSnooze:
		return prov.Archive(ctx, ids)
	case OpUnarchive:
		return prov.Unarchive(ctx, ids)
	case OpSnoozeWake:
		if err := prov.Unarchive(ctx, ids); err != nil {
			return err
		}
		if p := ops[0].Payload.Snooze; p != nil && p.MarkUnread {
			return prov.ModifyFlags(ctx, ids, []provider.Flag{provider.FlagUnread}, nil)
		}
		return nil
	case OpTrash:
		return prov.Trash(ctx, ids)
	case OpUnspam:
		return prov.Unspam(ctx, ids)
	case OpSpam:
		return prov.Spam(ctx, ids)
	case OpSendReply:
		if ops[0].Payload.Reply == nil || len(ops[0].ThreadIDs) == 0 {
			return errdefs.Wrap(errdefs.KindPermanent, errBadPayload)
		}
		return prov.SendReply(ctx, ops[0].ThreadIDs[0], *ops[0].Payload.Reply)
	case OpCompose:
		if ops[0].Payload.Compose == nil {
			return errdefs.Wrap(errdefs.KindPermanent, errBadPayload)
		}
		return prov.Compose(ctx, *ops[0].Payload.Compose)
	default:
		return errdefs.Wrap(errdefs.KindPermanent, errBadPayload)
	}
}

// handleFailure applies the retry policy keyed on the error kind.
func (e *Executor) handleFailure(ctx context.Context, ops []Op, ids []int, callErr error) error {
	switch kind := errdefs.KindOf(callErr); {
	case kind == errdefs.KindAuth:
		// Pause the account; ops go back to pending untouched and wait
		// for re-authentication.
		return withTx(ctx, e.db, func(tx *ent.Tx) error {
			if _, err := tx.PendingOp.Update().
				Where(pendingop.IDIn(ids...)).
				SetState(pendingop.StatePending).
				SetLastError(callErr.Error()).
				Save(ctx); err != nil {
				return err
			}
			if _, err := tx.Account.UpdateOneID(e.accountID).
				SetStatus(account.StatusAuthError).
				SetLastError(callErr.Error()).
				Save(ctx); err != nil {
				return err
			}
			e.bus.Publish("account.auth", map[string]any{"accountId": e.accountID.String()})
			return nil
		})

	case errdefs.Retryable(callErr) && ops[0].Attempts+1 < MaxAttempts:
		attempt := ops[0].Attempts + 1
		delay := e.backoff[min(attempt-1, len(e.backoff)-1)]
		_, err := e.db.PendingOp.Update().
			Where(pendingop.IDIn(ids...)).
			SetState(pendingop.StatePending).
			SetAttempts(attempt).
			SetNextAttemptAt(e.now().Add(delay).UTC()).
			SetLastError(callErr.Error()).
			Save(ctx)
		return err

	default:
		// Permanent (or retries exhausted): revert optimistic state,
		// mark failed, tell the user.
		err := withTx(ctx, e.db, func(tx *ent.Tx) error {
			for _, op := range ops {
				if err := revertLocal(ctx, tx, op); err != nil {
					return err
				}
			}
			_, err := tx.PendingOp.Update().
				Where(pendingop.IDIn(ids...)).
				SetState(pendingop.StateFailed).
				SetAttempts(ops[0].Attempts + 1).
				SetLastError(callErr.Error()).
				Save(ctx)
			return err
		})
		if err != nil {
			return err
		}
		e.bus.Publish("op.failed", map[string]any{
			"accountId": e.accountID.String(),
			"opType":    string(ops[0].Type),
			"error":     callErr.Error(),
		})
		e.bus.Publish("threads.changed", map[string]any{"accountId": e.accountID.String()})
		return nil
	}
}

func mergeThreadIDs(ops []Op) []string {
	seen := map[string]bool{}
	var out []string
	for _, op := range ops {
		for _, id := range op.ThreadIDs {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}
