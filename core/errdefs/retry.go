package errdefs

import (
	"errors"
	"time"
)

// RetryAfter preserves a provider's minimum delay through wrapped errors.
// A zero result means the caller should use its normal scheduling policy.
func RetryAfter(err error) time.Duration {
	var hint interface{ RetryAfter() time.Duration }
	if errors.As(err, &hint) {
		return max(0, hint.RetryAfter())
	}
	return 0
}
