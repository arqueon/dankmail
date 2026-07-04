package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Contact feeds the compose autocomplete. Two sources, kept as separate
// rows and merged at query time:
//
//	mail   — correspondents inferred from cached message From/To/Cc
//	         (rebuilt periodically; no extra permissions);
//	google — People API contacts + "other contacts" (requires the
//	         contacts scopes and a re-consent; refreshed daily).
type Contact struct {
	ent.Schema
}

func (Contact) Fields() []ent.Field {
	return []ent.Field{
		field.String("email"),
		field.String("name").Default(""),
		field.Enum("source").Values("mail", "google"),
		// weight ranks suggestions (mail: occurrence count).
		field.Int("weight").Default(0),
		field.Time("last_seen").Default(time.Now),
	}
}

func (Contact) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("account", Account.Type).Ref("contacts").Unique().Required(),
	}
}

func (Contact) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("email", "source").Edges("account").Unique(),
		index.Fields("weight"),
	}
}
