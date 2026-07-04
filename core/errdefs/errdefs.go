// Package errdefs defines the typed error kinds every provider must wrap
// its failures in. The pending-op queue keys its retry policy on the kind:
// network and rate-limit errors retry with backoff, auth errors pause the
// account and surface a re-auth notification, permanent errors fail the op.
package errdefs

import (
	"errors"
	"fmt"
)

// Kind classifies a provider error for the retry policy.
type Kind int

const (
	// KindUnknown is the zero value; treated as retryable once, then permanent.
	KindUnknown Kind = iota
	// KindAuth: credentials expired or revoked. Pauses the account until
	// the user re-authenticates; ops stay pending.
	KindAuth
	// KindRateLimit: provider throttling. Retry with the provider-suggested
	// delay if any, else exponential backoff.
	KindRateLimit
	// KindNetwork: transient transport failure. Retry with backoff.
	KindNetwork
	// KindPermanent: the operation can never succeed (invalid ID, message
	// gone, unsupported by server). Fail the op and revert local state.
	KindPermanent
)

// Error wraps an underlying error with a Kind.
type Error struct {
	Kind Kind
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v", e.Kind, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

func (k Kind) String() string {
	switch k {
	case KindAuth:
		return "auth"
	case KindRateLimit:
		return "rate-limit"
	case KindNetwork:
		return "network"
	case KindPermanent:
		return "permanent"
	default:
		return "unknown"
	}
}

// Wrap attaches a kind to err. Returns nil if err is nil.
func Wrap(kind Kind, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: kind, Err: err}
}

// KindOf extracts the Kind from err, or KindUnknown if it carries none.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindUnknown
}

// Retryable reports whether the queue should retry an op that failed with err.
func Retryable(err error) bool {
	switch KindOf(err) {
	case KindRateLimit, KindNetwork, KindUnknown:
		return true
	default:
		return false
	}
}
