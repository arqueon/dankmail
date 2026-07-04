package gmail

import (
	"errors"
	"net"

	"github.com/arqueon/dankmail/core/errdefs"
	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
)

// classify wraps a raw Gmail/transport error with the errdefs kind the
// pending-op queue keys its retry policy on:
//
//	401 or oauth2 invalid_grant                  → KindAuth
//	429, 403 with a rate-limit reason            → KindRateLimit
//	5xx and transport (net) errors               → KindNetwork
//	other 4xx                                    → KindPermanent
//	anything else                                → KindUnknown
//
// The history-404 cursor expiry is handled inside Sync (full resync), it
// never reaches classify as a returned error.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var ge *googleapi.Error
	if errors.As(err, &ge) {
		switch {
		case ge.Code == 401:
			return errdefs.Wrap(errdefs.KindAuth, err)
		case ge.Code == 429,
			ge.Code == 403 && hasRateLimitReason(ge):
			return errdefs.Wrap(errdefs.KindRateLimit, err)
		case ge.Code >= 500:
			return errdefs.Wrap(errdefs.KindNetwork, err)
		default:
			return errdefs.Wrap(errdefs.KindPermanent, err)
		}
	}
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		if re.ErrorCode == "invalid_grant" ||
			(re.Response != nil && re.Response.StatusCode == 401) {
			return errdefs.Wrap(errdefs.KindAuth, err)
		}
		// Other token-endpoint failures (5xx, timeouts) are transient.
		return errdefs.Wrap(errdefs.KindNetwork, err)
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return errdefs.Wrap(errdefs.KindNetwork, err)
	}
	return errdefs.Wrap(errdefs.KindUnknown, err)
}

func hasRateLimitReason(ge *googleapi.Error) bool {
	for _, item := range ge.Errors {
		switch item.Reason {
		case "rateLimitExceeded", "userRateLimitExceeded":
			return true
		}
	}
	return false
}

// isNotFound reports whether err is a Gmail 404 (thread/message gone, or
// an expired history cursor).
func isNotFound(err error) bool {
	var ge *googleapi.Error
	return errors.As(err, &ge) && ge.Code == 404
}
