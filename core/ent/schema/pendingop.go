package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PendingOp is one queued user action awaiting execution against the
// provider. The UI applies the effect locally at enqueue time (optimistic);
// the per-account executor drains the queue with retries and exponential
// backoff (max 5 attempts), then marks failed, notifies, and reverts the
// local state. While an op for a thread is pending/inflight, sync must not
// overwrite that thread's local state.
type PendingOp struct {
	ent.Schema
}

func (PendingOp) Fields() []ent.Field {
	return []ent.Field{
		// Provider-native thread IDs the op applies to (batchable ops
		// carry several). Empty for compose.
		field.JSON("provider_thread_ids", []string{}).Default([]string{}),
		field.Enum("op_type").Values(
			"mark_read", "mark_unread",
			"star", "unstar",
			"archive", "unarchive", "trash", "unspam",
			"snooze", "snooze_wake",
			"send_reply", "compose",
		),
		// Op-specific data: ReplyDraft/ComposeDraft JSON, snooze deadline,
		// pre-op thread state for revert on failure.
		field.JSON("payload", map[string]any{}).Default(map[string]any{}),
		field.Time("created_at").Default(utcNow).Immutable(),
		field.Int("attempts").Default(0),
		field.Time("next_attempt_at").Optional().Nillable(),
		field.String("last_error").Default(""),
		field.Enum("state").Values("pending", "inflight", "done", "failed").Default("pending"),
	}
}

func (PendingOp) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("account", Account.Type).Ref("pending_ops").Unique().Required(),
	}
}

func (PendingOp) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("state"),
		index.Fields("created_at"),
	}
}
