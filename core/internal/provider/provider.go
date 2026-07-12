// Package provider defines the contract every mail backend (Gmail API,
// generic IMAP, ...) must satisfy. The daemon core — sync loop, pending-op
// queue, notifier, UI — talks exclusively to this interface and adapts to
// what each backend declares via capability flags. UI and queue code must
// check capabilities up front; a provider method outside the declared set
// is a programming error, never a runtime fallback path.
package provider

import "context"

// Capability is a bitmask of features a Provider supports. The UI hides or
// adapts actions the provider lacks (degradación por capacidades): a missing
// capability must never surface as a runtime failure.
type Capability uint32

const (
	// CapModifyFlags: toggle read/unread and star on threads.
	CapModifyFlags Capability = 1 << iota
	// CapArchive: remove threads from the inbox without deleting them.
	CapArchive
	// CapTrash: move threads to the provider trash. Permanent deletion is
	// deliberately absent from this interface and must not be added.
	CapTrash
	// CapServerSnooze: server-side snooze. Reserved; no provider sets it
	// today — snooze is always simulated locally (archive + scheduled wake).
	CapServerSnooze
	// CapSendReply: reply in-thread (plain text only).
	CapSendReply
	// CapCompose: send a new plain-text message.
	CapCompose
	// CapPush: real push notification of changes (Gmail Pub/Sub watch,
	// IMAP IDLE). Without it the daemon falls back to polling.
	CapPush
	// CapDeepLink: WebLink returns a URL to the message/thread in the
	// provider webmail (not just the mailbox root).
	CapDeepLink
	// CapHistorySync: incremental sync via cursor (Gmail historyId, IMAP
	// CONDSTORE MODSEQ). Without it Sync returns a full diff each time.
	CapHistorySync
	// CapUnspam: rescue threads from the spam folder back to the inbox
	// (Gmail: remove SPAM + add INBOX; Graph: move junkemail → inbox).
	CapUnspam
	// CapSpam: move threads to the spam folder.
	CapSpam
)

// Has reports whether all bits in want are set.
func (c Capability) Has(want Capability) bool { return c&want == want }

// Flag is a provider-neutral message/thread flag. Providers map flags to
// their native representation (Gmail labels UNREAD/STARRED, IMAP \Seen/\Flagged).
type Flag string

const (
	// FlagUnread marks a thread as not yet read. Note the polarity: Gmail
	// models "unread" (UNREAD label) while IMAP models "read" (\Seen); the
	// IMAP provider inverts internally.
	FlagUnread Flag = "unread"
	// FlagStarred marks a thread as starred/flagged.
	FlagStarred Flag = "starred"
)

// Changes is the result of one Sync call: everything that changed remotely
// since the given cursor. IDs are provider-native thread/message IDs; the
// sync engine maps them to local rows.
type Changes struct {
	// FullResync signals the cursor was invalid/expired (Gmail history 404,
	// IMAP UIDVALIDITY change) and this payload is a complete snapshot:
	// the local cache for the account must be reconciled against it, and
	// threads absent from it treated as gone.
	FullResync bool
	// Backfill marks deltas that ingest OLD mail on purpose (remote
	// search results): upsert normally but never notify, never prune.
	Backfill bool
	// Upserted holds new or modified threads with their messages.
	Upserted []ThreadDelta
	// RemovedThreadIDs lists provider thread IDs that left the monitored
	// scope (deleted, or no longer carrying any monitored label/folder).
	RemovedThreadIDs []string
}

// ThreadDelta is the remote state of one thread after a sync.
type ThreadDelta struct {
	ThreadID     string // provider-native thread ID
	Subject      string
	Snippet      string
	LastMessage  int64    // unix seconds of newest message
	Participants []string // display forms, e.g. "Ada <ada@example.org>"
	Unread       bool
	Starred      bool
	InInbox      bool
	Labels       []string // provider labels / folder names beyond the basics
	MessageCount int
	// Messages carries the messages the provider fetched for this delta.
	// It may be partial (e.g. only new messages on incremental sync).
	Messages []MessageDelta
}

// AttachmentMeta describes an attachment WITHOUT its content: dankmail
// never downloads attachment bodies (spec §1); the metadata lets the UI
// show what a message carries so the user can decide to open the webmail.
type AttachmentMeta struct {
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"` // bytes, provider-reported
}

// MessageDelta is the remote state of one message.
type MessageDelta struct {
	MessageID       string // provider-native message ID
	RFC822MessageID string // Message-ID header, for deep links and reply threading
	From            string
	To              []string
	Cc              []string
	Date            int64 // unix seconds
	Snippet         string
	BodyText        string // plain text, already truncated by the provider to the configured cap
	// Attachments carries metadata only — never content.
	Attachments []AttachmentMeta
	// ReplyHeaders holds the minimal headers needed to build a threaded
	// reply later (References, In-Reply-To source). Keyed by canonical
	// header name.
	ReplyHeaders map[string]string
}

// Canonical keys providers must use in MessageDelta.ReplyHeaders.
const (
	HeaderSubject    = "Subject"
	HeaderReferences = "References"
	HeaderReplyTo    = "Reply-To"
)

// ReplyDraft is a plain-text reply to an existing thread. The provider is
// responsible for building the MIME message: quoting is NOT included, the
// body is sent as-is; In-Reply-To/References come from the original message.
type ReplyDraft struct {
	// InReplyToMessageID is the provider-native ID of the message being
	// answered (normally the newest in the thread).
	InReplyToMessageID string
	Body               string
	// ReplyAll includes the original To/Cc recipients besides the sender.
	ReplyAll bool
}

// ComposeDraft is a minimal new message: recipients, subject, plain body.
// No attachments, no HTML, no editable Cc/Bcc — by design.
type ComposeDraft struct {
	To      []string
	Subject string
	Body    string
}

// RemoteSearcher is an optional interface (dcal-style: assert with a
// type switch, never assume): providers whose backend can search the
// FULL mailbox history server-side implement it. Results are deltas
// meant to be ingested as backfill — Changes.Backfill must be true so
// the reconciler suppresses notifications for old mail.
type RemoteSearcher interface {
	SearchRemote(ctx context.Context, query string, limit int) (Changes, error)
}

// Provider is implemented once per account type. Implementations must be
// safe for concurrent use: the sync loop and the pending-op executor call
// them from different goroutines.
//
// All mutating methods take provider-native thread IDs and must be
// idempotent where the backend allows it (re-archiving an archived thread
// is a no-op, not an error). Errors must be wrapped with the errdefs kinds
// so the queue can decide between retry and permanent failure.
type Provider interface {
	// ID returns the stable identifier of the account this instance serves
	// (the local Account row ID, not the email address).
	ID() string

	// Capabilities returns the static feature set of this provider for
	// this account. It must not change during the lifetime of the instance.
	Capabilities() Capability

	// Sync returns remote changes since cursor and the new cursor to
	// persist. An empty cursor requests an initial full sync (Changes with
	// FullResync=true). Providers without CapHistorySync ignore the cursor
	// and always return a full snapshot.
	Sync(ctx context.Context, cursor string) (Changes, string, error)

	// ModifyFlags adds and removes flags on the given threads.
	// Requires CapModifyFlags.
	ModifyFlags(ctx context.Context, threadIDs []string, add, remove []Flag) error

	// Archive removes the threads from the inbox. Requires CapArchive.
	Archive(ctx context.Context, threadIDs []string) error

	// Unarchive puts the threads back in the inbox (Gmail: add INBOX;
	// IMAP: MOVE back from the archive folder). Required by the local
	// snooze wake-up and the star→inbox action hook. Requires CapArchive.
	Unarchive(ctx context.Context, threadIDs []string) error

	// Trash moves the threads to the provider trash (Gmail TRASH label,
	// IMAP \Trash folder). Never permanent deletion. Requires CapTrash.
	Trash(ctx context.Context, threadIDs []string) error

	// Unspam rescues the threads from the spam folder back to the inbox
	// ("not spam"). Requires CapUnspam.
	Unspam(ctx context.Context, threadIDs []string) error

	// Spam moves the threads to the spam folder. Requires CapSpam.
	Spam(ctx context.Context, threadIDs []string) error

	// SendReply sends a plain-text reply on the given thread, threading it
	// correctly (In-Reply-To/References, provider thread association).
	// Requires CapSendReply.
	SendReply(ctx context.Context, threadID string, r ReplyDraft) error

	// Compose sends a new plain-text message. Requires CapCompose.
	Compose(ctx context.Context, m ComposeDraft) error

	// WebLink returns a URL that opens the given message/thread in the
	// provider webmail, and whether such a link exists. With CapDeepLink
	// the URL targets the specific thread/message; without it providers
	// may still return a mailbox-level URL (ok=true) or nothing (ok=false).
	WebLink(threadID, messageID string) (url string, ok bool)
}
