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

import "context"

// Engine coordinates one goroutine pair (sync loop + queue executor) per
// active account.
type Engine struct {
	// TODO(anillo1): repo handle, provider registry, notifier, event bus,
	// per-account tickers, snooze-wake scheduler.
}

func NewEngine() *Engine { return &Engine{} }

// Run blocks until ctx is cancelled.
// TODO(anillo1): start per-account loops; rebuild the snooze scheduler
// from Thread.snoozed_until on startup.
func (e *Engine) Run(ctx context.Context) error {
	panic("sync: Run not implemented yet (anillo 1)")
}

// Backoff policy for the op queue: 5 attempts max, delays roughly
// 2s, 8s, 30s, 2m, 5m (rate-limit errors honor the provider's Retry-After
// when present). After the 5th failure: state=failed, revert, notify.
const MaxAttempts = 5
