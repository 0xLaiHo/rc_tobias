package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Notification is the durable source of truth for one requested supplier
// callback. Redis messages reference this row by ID instead of copying payloads.
type Notification struct {
	ent.Schema
}

func (Notification) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "notifications"}}
}

func (Notification) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.String("url").NotEmpty(),
		field.String("method").Default("POST"),
		field.JSON("headers", map[string]string{}).Optional(),
		field.Bytes("body").Optional(),
		field.Enum("status").Values("pending", "delivering", "succeeded", "retrying", "failed").Default("pending"),
		field.Int("attempt_count").Default(0),
		field.Int("max_attempts").Default(5),
		field.String("last_error").Optional(),
		field.Time("next_attempt_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
