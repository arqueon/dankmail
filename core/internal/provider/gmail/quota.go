package gmail

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"google.golang.org/api/googleapi"
)

// Gmail's per-user, per-project budget is 6,000 units/minute. Use 2,400
// per process, leaving headroom for another desktop and interactive actions.
// https://developers.google.com/workspace/gmail/api/reference/quota
const quotaUnitsPerSecond = 40
const maxReadRetries = 5

type quotaGate struct {
	mu           sync.Mutex
	next         time.Time
	blockedUntil time.Time
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
func (q *quotaGate) wait(ctx context.Context, cost int) error {
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

func (q *quotaGate) cooldown(delay time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	until := q.now().Add(delay)
	if until.After(q.blockedUntil) {
		q.blockedUntil = until
	}
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
	if limited {
		delay = time.Minute * time.Duration(1<<min(attempt, 1))
	}
	// Google's Retry-After is a lower bound, including HTTP-date values.
	if seconds, err := strconv.ParseInt(e.Header.Get("Retry-After"), 10, 32); err == nil && seconds > 0 {
		delay = max(delay, time.Duration(seconds)*time.Second)
	} else if when, err := http.ParseTime(e.Header.Get("Retry-After")); err == nil {
		delay = max(delay, when.Sub(now))
	}
	return delay, true
}

// Retry only reads. Holding the provider's accumulated deltas while retrying
// the failing page/thread prevents a quota error from restarting the import.
// Non-idempotent sends are paced but never automatically replayed here.
func readCall[T any](ctx context.Context, r *realAPI, cost int, call func() (*T, error)) (*T, error) {
	for attempt := 0; ; attempt++ {
		if err := r.quota.wait(ctx, cost); err != nil {
			return nil, err
		}
		result, err := call()
		if err == nil {
			return result, nil
		}
		delay, retry := readRetryDelay(err, attempt, r.quota.now())
		if !retry || attempt >= maxReadRetries {
			return nil, err
		}
		r.quota.cooldown(delay + time.Duration(rand.Int64N(int64(time.Second))))
	}
}
