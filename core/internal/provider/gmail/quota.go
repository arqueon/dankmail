package gmail

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"google.golang.org/api/googleapi"
)

// Gmail uses weighted quota units. Keep 2,400 units/minute per account,
// leaving headroom for another desktop and interactive actions.
// https://developers.google.com/workspace/gmail/api/reference/quota
const quotaUnitsPerSecond = 40
const maxReadRetries = 5

// A send costs 100 units (2.5 seconds of pacing). Allow that normal gap,
// but defer longer waits rather than holding a sync or user action open.
const maxInlineDelay = 3 * time.Second
const maxInlineRetryWait = 5 * time.Second

// Reload rebuilds providers. Quota and server cooldowns belong to the
// account for the lifetime of this process, not to a provider instance.
var accountQuotas sync.Map // account ID -> *quotaGate

func accountQuota(id string) *quotaGate {
	if id == "" {
		return newQuotaGate()
	}
	value, _ := accountQuotas.LoadOrStore(id, newQuotaGate())
	return value.(*quotaGate)
}

type deferredRetry struct {
	until time.Time
	now   func() time.Time
	cause error
}

func (e *deferredRetry) Error() string {
	return fmt.Sprintf("Gmail retry deferred for %s", e.RetryAfter())
}
func (e *deferredRetry) Unwrap() error             { return e.cause }
func (e *deferredRetry) RetryAfter() time.Duration { return max(0, e.until.Sub(e.now())) }

type quotaGate struct {
	mu           sync.Mutex
	next         time.Time
	blockedUntil time.Time
	cause        error
	now          func() time.Time
	sleep        func(context.Context, time.Duration) error
}

func newQuotaGate() *quotaGate {
	return &quotaGate{now: time.Now, sleep: sleepContext}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Reserve only once a request can start. Waiters re-check shared cooldowns
// so one rate-limit response also slows concurrent search and user actions.
func (q *quotaGate) wait(ctx context.Context, cost int, spend func(time.Duration) bool) error {
	started := q.now()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		q.mu.Lock()
		now := q.now()
		ready := q.next
		if q.blockedUntil.After(ready) {
			ready = q.blockedUntil
		}
		delay := ready.Sub(now)
		cooldown := max(0, q.blockedUntil.Sub(now))
		if delay > 0 && (delay > maxInlineDelay || now.Sub(started)+delay > maxInlineDelay ||
			(cooldown > 0 && spend != nil && !spend(cooldown))) {
			err := &deferredRetry{until: ready, now: q.now, cause: q.cause}
			q.mu.Unlock()
			return err
		}
		if delay <= 0 {
			q.next = now.Add(time.Duration(cost) * time.Second / quotaUnitsPerSecond)
			q.mu.Unlock()
			return nil
		}
		q.mu.Unlock()
		if err := q.sleep(ctx, delay); err != nil {
			return err
		}
	}
}

func (q *quotaGate) cooldown(delay time.Duration, cause error) *deferredRetry {
	q.mu.Lock()
	defer q.mu.Unlock()
	until := q.now().Add(delay)
	if until.After(q.blockedUntil) {
		q.blockedUntil = until
		q.cause = cause
	}
	return &deferredRetry{until: q.blockedUntil, now: q.now, cause: q.cause}
}

func quotaLimited(err error) bool {
	var e *googleapi.Error
	if !errors.As(err, &e) {
		return false
	}
	if e.Code == http.StatusTooManyRequests {
		return true
	}
	if e.Code != http.StatusForbidden {
		return false
	}
	for _, detail := range e.Errors {
		if detail.Reason == "rateLimitExceeded" || detail.Reason == "userRateLimitExceeded" {
			return true
		}
	}
	return false
}

func readRetryDelay(err error, attempt int, now time.Time) (time.Duration, bool) {
	var e *googleapi.Error
	if !errors.As(err, &e) {
		return 0, false
	}
	limited := quotaLimited(err)
	if !limited && e.Code != 500 && e.Code != 502 && e.Code != 503 && e.Code != 504 {
		return 0, false
	}
	delay := time.Second * time.Duration(1<<min(attempt, 6))
	// Google's Retry-After is a lower bound, including HTTP-date values.
	if seconds, err := strconv.ParseInt(e.Header.Get("Retry-After"), 10, 32); err == nil && seconds > 0 {
		delay = max(delay, time.Duration(seconds)*time.Second)
	} else if when, err := http.ParseTime(e.Header.Get("Retry-After")); err == nil {
		delay = max(delay, when.Sub(now))
	}
	return delay, true
}

// retryBudget belongs to one high-level operation, spanning every page and
// thread. A cached Provider serves concurrent operations, so its budget must
// not be a Provider field or reset at every individual HTTP request.
type retryBudget struct {
	mu   sync.Mutex
	used time.Duration
}
type retryBudgetKey struct{}

func withRetryBudget(ctx context.Context) context.Context {
	if ctx.Value(retryBudgetKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, retryBudgetKey{}, &retryBudget{})
}

func (r *realAPI) waitQuota(ctx context.Context, cost int) error {
	return r.quota.wait(ctx, cost, func(delay time.Duration) bool {
		b, _ := ctx.Value(retryBudgetKey{}).(*retryBudget)
		if b == nil {
			return true
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		if delay > maxInlineRetryWait-b.used {
			return false
		}
		b.used += delay
		return true
	})
}

// Read retries preserve pages already fetched in this operation. A long
// cooldown returns a scheduling hint instead of sleeping inside the call.
func readCall[T any](ctx context.Context, r *realAPI, cost int, call func() (*T, error)) (*T, error) {
	ctx = withRetryBudget(ctx)
	for attempt := 0; ; attempt++ {
		if err := r.waitQuota(ctx, cost); err != nil {
			return nil, err
		}
		result, err := call()
		if err == nil {
			return result, nil
		}
		deferred := r.deferFailure(err, attempt)
		if deferred == nil {
			return nil, err
		}
		if attempt >= maxReadRetries {
			return nil, deferred
		}
	}
}

// Record cooldowns on writes and exhausted reads too. This does not replay
// a write; the caller receives the error and its earliest retry time.
func (r *realAPI) deferFailure(err error, attempt int) error {
	delay, retry := readRetryDelay(err, attempt, r.quota.now())
	if !retry {
		return nil
	}
	delay += time.Duration(rand.Int64N(int64(time.Second)))
	return r.quota.cooldown(delay, err)
}
