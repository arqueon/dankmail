package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/arqueon/dankmail/core/internal/provider"
)

// Message is one message inside a cached thread: metadata plus a truncated
// plain-text body and attachment METADATA. No HTML, no attachment
// content, no blobs — anything richer is read in the webmail via deep link.
type Message struct {
	ent.Schema
}

func (Message) Fields() []ent.Field {
	return []ent.Field{
		field.String("provider_message_id"),
		// Message-ID header; used for rfc822msgid deep links and reply
		// threading (In-Reply-To/References).
		field.String("rfc822_message_id").Default(""),
		field.String("from").Default(""),
		field.JSON("to", []string{}).Default([]string{}),
		field.JSON("cc", []string{}).Default([]string{}),
		field.Time("date"),
		field.String("snippet").Default(""),
		// Plain text, truncated at ingest to the configured cap
		// (default 32 KiB).
		field.Text("body_text").Default(""),
		// Minimal headers required to build a threaded reply later
		// (References, In-Reply-To source), keyed by canonical name.
		field.JSON("reply_headers", map[string]string{}).Default(map[string]string{}),
		// Attachment metadata only (filename/mime/size) — content is
		// never downloaded nor stored (spec §1).
		field.JSON("attachments", []provider.AttachmentMeta{}).Optional(),
	}
}

func (Message) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("thread", Thread.Type).Ref("messages").Unique().Required(),
	}
}

func (Message) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_message_id").Edges("thread").Unique(),
		index.Fields("date"),
	}
}
