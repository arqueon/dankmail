package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Secret stores account secrets in the database as a fallback when
// the system keyring is unavailable.
type Secret struct {
	ent.Schema
}

func (Secret) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("key").NotEmpty(),
		field.Bytes("value").Sensitive(),
		field.Time("created_at").Default(utcNow).Immutable(),
		field.Time("updated_at").Default(utcNow).UpdateDefault(utcNow),
	}
}

func (Secret) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("account", Account.Type).
			Ref("secrets").
			Unique().
			Required(),
	}
}

func (Secret) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("account").Fields("key").Unique(),
	}
}
