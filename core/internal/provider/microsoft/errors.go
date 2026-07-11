package microsoft

import (
	"errors"
	"fmt"
	"net/http"
	"unicode/utf8"

	"github.com/arqueon/dankmail/core/errdefs"
)

// graphError is an HTTP-level failure from the Graph API.
type graphError struct {
	Status int
	Code   string // Graph error.code, e.g. "SyncStateNotFound"
	Msg    string
}

func (e *graphError) Error() string {
	return fmt.Sprintf("graph: %d %s: %s", e.Status, e.Code, e.Msg)
}

// classify maps Graph failures onto the errdefs kinds driving the
// queue's retry policy (mirror of gmail/errors.go).
func classify(err error) error {
	var ge *graphError
	if !errors.As(err, &ge) {
		var kerr *errdefs.Error
		if errors.As(err, &kerr) {
			return err // already classified (e.g. token source)
		}
		return errdefs.Wrap(errdefs.KindNetwork, err)
	}
	switch {
	case ge.Status == http.StatusUnauthorized:
		return errdefs.Wrap(errdefs.KindAuth, err)
	case ge.Status == http.StatusForbidden && ge.Code == "ErrorAccessDenied":
		return errdefs.Wrap(errdefs.KindAuth, err)
	case ge.Status == http.StatusTooManyRequests:
		return errdefs.Wrap(errdefs.KindRateLimit, err)
	case ge.Status >= 500:
		return errdefs.Wrap(errdefs.KindNetwork, err)
	default:
		return errdefs.Wrap(errdefs.KindPermanent, err)
	}
}

func isNotFound(err error) bool {
	var ge *graphError
	return errors.As(err, &ge) && ge.Status == http.StatusNotFound
}

// isGone reports an expired delta token (Graph answers 410 with
// code SyncStateNotFound); the caller restarts with a full sync.
func isGone(err error) bool {
	var ge *graphError
	return errors.As(err, &ge) && ge.Status == http.StatusGone
}

// truncateAtRune cuts s to at most capBytes bytes without splitting a
// UTF-8 rune (same helper the Gmail provider carries).
func truncateAtRune(s string, capBytes int) string {
	if capBytes <= 0 || len(s) <= capBytes {
		return s
	}
	cut := capBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
