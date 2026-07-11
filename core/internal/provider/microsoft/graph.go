// Package microsoft implements provider.Provider for Outlook.com /
// Microsoft 365 mailboxes over the Microsoft Graph API (see
// docs/design/microsoft-provider.md). Graph is message-centric; the
// provider groups messages by conversationId to present the
// thread-centric contract the daemon core expects.
package microsoft

import (
	"context"

	"github.com/arqueon/dankmail/core/internal/provider"
)

// graphMessage is the provider-internal view of one Graph message —
// everything the provider reads and nothing more. The real client maps
// Graph JSON onto it; the test fake fabricates it directly.
type graphMessage struct {
	ID                string
	ConversationID    string
	InternetMessageID string // RFC 822 Message-ID header
	Subject           string
	BodyPreview       string
	From              string // display form, "Ada <ada@example.org>"
	To                []string
	Cc                []string
	ReceivedAt        int64 // unix seconds
	IsRead            bool
	Flagged           bool
	ParentFolderID    string
	HasAttachments    bool
	BodyText          string // plain text (client distills text/html)
	// Headers carries the reply-threading headers (References, Reply-To)
	// from internetMessageHeaders, keyed by canonical name.
	Headers     map[string]string
	Attachments []provider.AttachmentMeta
	// Removed marks a delta "@removed" entry (only ID is populated).
	Removed bool
}

// deltaPage is one page of a folder message-delta round.
type deltaPage struct {
	Messages []*graphMessage
	// NextLink continues the current round; DeltaLink (only on the last
	// page) is the cursor for the next round.
	NextLink  string
	DeltaLink string
}

// Well-known Graph folder names the provider monitors or moves to.
// Graph accepts these directly in URLs; FolderIDs resolves their real
// IDs for parentFolderId comparisons.
const (
	folderInbox   = "inbox"
	folderJunk    = "junkemail"
	folderArchive = "archive"
	folderTrash   = "deleteditems"
)

// graphAPI is the seam between the provider and the Graph REST client —
// the fake in tests implements exactly this (anillo1 §2 pattern).
type graphAPI interface {
	// GetProfile returns the mailbox address (GET /me).
	GetProfile(ctx context.Context) (string, error)
	// DeltaMessages runs one page of GET /me/mailFolders/{folder}/messages/delta.
	// link "" starts a full round for the folder; otherwise pass the
	// nextLink or deltaLink verbatim.
	DeltaMessages(ctx context.Context, folder, link string) (deltaPage, error)
	// GetMessage fetches one message (used to resolve @removed IDs to
	// their conversation; returns not-found if truly deleted).
	GetMessage(ctx context.Context, id string) (*graphMessage, error)
	// ListConversation returns every message of a conversation, with
	// bodies and reply headers, oldest first.
	ListConversation(ctx context.Context, convID string) ([]*graphMessage, error)
	// PatchMessage updates message properties (isRead, flag).
	PatchMessage(ctx context.Context, id string, body map[string]any) error
	// MoveMessage moves a message to a well-known destination folder.
	MoveMessage(ctx context.Context, id, destFolder string) error
	// SendMail submits a raw MIME message (POST /me/sendMail).
	SendMail(ctx context.Context, mime []byte) error
	// FolderIDs resolves the well-known folder names above to their
	// mailbox-specific IDs.
	FolderIDs(ctx context.Context) (map[string]string, error)
}
