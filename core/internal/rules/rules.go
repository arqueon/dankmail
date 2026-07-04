// Package rules implements action hooks: a declarative layer that expands
// one user action into several PendingOps before they are enqueued
// (e.g. "trash" also enqueues "mark_read"). It is a pure function over op
// descriptors — no I/O — so it is trivially unit-testable, and it is the
// extension point for future user-defined rules (anillo 3).
package rules

// Trigger identifies the user action that may expand.
type Trigger string

const (
	TriggerPreviewOpened Trigger = "preview_opened"
	TriggerReplySent     Trigger = "reply_sent"
	TriggerTrash         Trigger = "trash"
	TriggerStar          Trigger = "star"
)

// Policies are the built-in chained actions (spec §5). Defaults follow the
// spec: the three mark-read hooks start on, unarchive-on-star starts off.
type Policies struct {
	MarkReadOnPreview bool `json:"markReadOnPreview"`
	MarkReadOnReply   bool `json:"markReadOnReply"`
	MarkReadOnTrash   bool `json:"markReadOnTrash"`
	UnarchiveOnStar   bool `json:"unarchiveOnStar"`
}

func DefaultPolicies() Policies {
	return Policies{
		MarkReadOnPreview: true,
		MarkReadOnReply:   true,
		MarkReadOnTrash:   true,
		UnarchiveOnStar:   false,
	}
}

// OpType mirrors the PendingOp op_type enum; declared here to keep the
// package free of ent imports.
type OpType string

// Expand returns the op types to enqueue alongside the primary op for a
// given trigger. The caller enqueues them in order, atomically.
// TODO(anillo2): wire into the enqueue path; today only defaults exist.
func (p Policies) Expand(t Trigger) []OpType {
	switch t {
	case TriggerPreviewOpened:
		if p.MarkReadOnPreview {
			return []OpType{"mark_read"}
		}
	case TriggerReplySent:
		if p.MarkReadOnReply {
			return []OpType{"mark_read"}
		}
	case TriggerTrash:
		if p.MarkReadOnTrash {
			return []OpType{"mark_read"}
		}
	case TriggerStar:
		if p.UnarchiveOnStar {
			return []OpType{"unarchive"}
		}
	}
	return nil
}
