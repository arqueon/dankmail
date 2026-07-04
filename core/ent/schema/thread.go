package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Thread is the local cache of a remote conversation. The remote server is
// the authority: rows here are overwritten by sync, except while a
// PendingOp for the thread is pending/inflight (optimistic-UI guard).
type Thread struct {
	ent.Schema
}

func (Thread) Fields() []ent.Field {
	return []ent.Field{
		field.String("provider_thread_id"),
		field.String("subject").Default(""),
		field.String("snippet").Default(""),
		field.Time("last_message_at"),
		// Display forms of the participants, e.g. "Ada <ada@example.org>".
		field.JSON("participants", []string{}).Default([]string{}),
		field.Bool("unread").Default(false),
		field.Bool("starred").Default(false),
		field.Bool("in_inbox").Default(true),
		field.JSON("labels", []string{}).Default([]string{}),
		// Local snooze deadline. Snooze is simulated: archived now, woken
		// by the daemon scheduler when this time passes. Null = not snoozed.
		field.Time("snoozed_until").Optional().Nillable(),
		field.Int("message_count").Default(0),
	}
}

func (Thread) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("account", Account.Type).Ref("threads").Unique().Required(),
		edge.To("messages", Message.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Thread) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_thread_id").Edges("account").Unique(),
		index.Fields("last_message_at"),
		index.Fields("snoozed_until"),
	}
}
