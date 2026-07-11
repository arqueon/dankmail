package sync

import (
	"time"

	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/internal/provider"
)

// OpType mirrors the PendingOp op_type enum (ent/schema/pendingop.go).
type OpType string

const (
	OpMarkRead   OpType = "mark_read"
	OpMarkUnread OpType = "mark_unread"
	OpStar       OpType = "star"
	OpUnstar     OpType = "unstar"
	OpArchive    OpType = "archive"
	OpUnarchive  OpType = "unarchive"
	OpTrash      OpType = "trash"
	OpUnspam     OpType = "unspam"
	OpSnooze     OpType = "snooze"
	OpSnoozeWake OpType = "snooze_wake"
	OpSendReply  OpType = "send_reply"
	OpCompose    OpType = "compose"
)

// batchable reports whether consecutive ops of this type against the same
// account may be coalesced into one provider call.
func (t OpType) batchable() bool {
	switch t {
	case OpSendReply, OpCompose:
		return false
	default:
		return true
	}
}

// Op is the domain view of a PendingOp row, free of ent types so the
// executor logic is testable in isolation.
type Op struct {
	ID        int
	AccountID uuid.UUID
	Type      OpType
	ThreadIDs []string // provider-native
	Payload   OpPayload
	Attempts  int
}

// ThreadState is the snapshot the optimistic layer needs to revert a
// failed op.
type ThreadState struct {
	Unread       bool       `json:"unread"`
	Starred      bool       `json:"starred"`
	InInbox      bool       `json:"inInbox"`
	SnoozedUntil *time.Time `json:"snoozedUntil,omitempty"`
}

// SnoozePayload parameterizes snooze and snooze_wake ops.
type SnoozePayload struct {
	Until time.Time `json:"until"`
	// MarkUnread: on wake, also flag the thread unread (configurable).
	MarkUnread bool `json:"markUnread,omitempty"`
}

// OpPayload carries op-specific data; unused fields stay empty.
type OpPayload struct {
	Reply   *provider.ReplyDraft   `json:"reply,omitempty"`
	Compose *provider.ComposeDraft `json:"compose,omitempty"`
	Snooze  *SnoozePayload         `json:"snooze,omitempty"`
	// Prev maps provider thread ID → pre-op state, captured at enqueue
	// time, used to revert the optimistic change if the op fails for good.
	Prev map[string]ThreadState `json:"prev,omitempty"`
}

// Registry resolves the provider instance serving an account. The daemon
// keeps it up to date as accounts are added/removed.
type Registry interface {
	Provider(accountID uuid.UUID) (provider.Provider, bool)
}
