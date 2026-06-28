package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// OutboxEvent is durable work for the relay. It exists to make accepting a
// notification and enqueueing delivery work a single PostgreSQL transaction.
type OutboxEvent struct {
	ent.Schema
}

func (OutboxEvent) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "outbox_events"}}
}

func (OutboxEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("notification_id", uuid.UUID{}),
		field.String("event_type").Default("notification.delivery_requested"),
		field.Enum("status").Values("pending", "publishing", "published", "failed").Default("pending"),
		field.Time("scheduled_at").Default(time.Now),
		field.String("stream_id").Optional(),
		field.String("locked_by").Optional(),
		field.Time("locked_at").Optional().Nillable(),
		field.String("last_error").Optional(),
		field.Time("published_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes keeps the relay's hot "claim due events" scan cheap: a partial index
// over only the rows the relay can ever pick (pending / publishing), ordered the
// same way ClaimDue scans them. Published/failed rows stay out of the index, so
// it does not grow with delivered history.
func (OutboxEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scheduled_at", "created_at").
			Annotations(entsql.IndexWhere("status IN ('pending', 'publishing')")),
	}
}
