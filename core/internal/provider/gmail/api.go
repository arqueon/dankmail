package gmail

import (
	"context"

	gmailv1 "google.golang.org/api/gmail/v1"
)

// gmailAPI is the thin seam over the generated Gmail client — everything
// the provider touches, and nothing more — so tests run against a fake
// with no HTTP. The real implementation lives in realapi.go.
//
// Implementations return the raw transport/googleapi errors; the Provider
// is responsible for classifying them with errdefs kinds.
type gmailAPI interface {
	// ListThreads returns one page of thread IDs carrying ALL the given
	// label IDs (Gmail AND-semantics; the provider calls it once per
	// monitored label to get the union).
	ListThreads(ctx context.Context, labelIDs []string, pageToken string) (ids []string, nextPageToken string, err error)

	// GetThread fetches a thread with format=full (messages, labels,
	// headers, bodies).
	GetThread(ctx context.Context, id string) (*gmailv1.Thread, error)

	// GetMessageMetadata fetches one message with format=metadata and the
	// headers needed to build a reply: Subject, From, To, Cc, Reply-To,
	// Message-ID, References.
	GetMessageMetadata(ctx context.Context, id string) (*gmailv1.Message, error)

	// ListHistory returns one page of history records starting after
	// startHistoryID.
	ListHistory(ctx context.Context, startHistoryID uint64, pageToken string) (*gmailv1.ListHistoryResponse, error)

	// ModifyThread adds/removes label IDs on one thread
	// (users.threads.modify; ops are thread-scoped).
	ModifyThread(ctx context.Context, threadID string, addLabelIDs, removeLabelIDs []string) error

	// SendMessage sends a raw RFC 822 message (users.messages.send). The
	// implementation base64url-encodes raw. An empty threadID sends a new
	// conversation; otherwise the message is associated to the thread.
	SendMessage(ctx context.Context, threadID string, raw []byte) error

	// GetProfile returns the account email address and current historyId.
	GetProfile(ctx context.Context) (email string, historyID uint64, err error)
}
