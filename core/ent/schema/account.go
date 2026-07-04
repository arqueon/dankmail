package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Account is a configured mail account (one per provider instance).
// Secrets (OAuth tokens, IMAP passwords) live in the system keyring keyed
// by the account ID — never in this table.
type Account struct {
	ent.Schema
}

func (Account) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.Enum("type").Values("gmail", "imap"),
		field.String("email"),
		field.String("display_name").Default(""),
		// Non-secret per-account config: IMAP/SMTP hosts and ports,
		// polling interval, webmail URL, special-folder overrides,
		// plain-text signature, body truncation cap.
		field.JSON("config", map[string]any{}).Default(map[string]any{}),
		// Provider sync cursor: Gmail historyId or IMAP MODSEQ state.
		// Empty means "initial full sync pending".
		field.String("sync_cursor").Default(""),
		field.Enum("status").Values("active", "paused", "auth_error").Default("active"),
		field.Time("last_sync_at").Optional().Nillable(),
		field.String("last_error").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Account) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("threads", Thread.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("pending_ops", PendingOp.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("notify_rules", NotifyRule.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
