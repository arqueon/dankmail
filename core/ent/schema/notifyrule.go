package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// NotifyRule configures notification behavior per account and per
// label/folder. Default policy when no rule matches: INBOX notifies at
// normal urgency, other monitored labels only update the unread counter.
type NotifyRule struct {
	ent.Schema
}

func (NotifyRule) Fields() []ent.Field {
	return []ent.Field{
		// Gmail label name or IMAP folder the rule applies to.
		field.String("label"),
		field.Bool("enabled").Default(true),
		// notify=false means silent: update counters only, no popup.
		field.Bool("notify").Default(true),
		// Sound name/path; empty = notification server default, "none" = mute.
		field.String("sound").Default(""),
		field.Enum("urgency").Values("low", "normal", "critical").Default("normal"),
	}
}

func (NotifyRule) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("account", Account.Type).Ref("notify_rules").Unique().Required(),
	}
}

func (NotifyRule) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("label").Edges("account").Unique(),
	}
}
