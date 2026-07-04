package sync

import (
	"time"

	"github.com/arqueon/dankmail/core/internal/provider"
)

func timeFromUnix(sec int64) time.Time {
	if sec <= 0 {
		return time.Unix(0, 0).UTC()
	}
	return time.Unix(sec, 0).UTC()
}

// replyHeaders keeps only the whitelisted headers we persist for building
// replies later.
func replyHeaders(m provider.MessageDelta) map[string]string {
	out := map[string]string{}
	for _, k := range []string{provider.HeaderSubject, provider.HeaderReferences, provider.HeaderReplyTo} {
		if v, ok := m.ReplyHeaders[k]; ok && v != "" {
			out[k] = v
		}
	}
	return out
}
