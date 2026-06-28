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

// DeliveryAttempt is the immutable audit trail for each outbound HTTP attempt.
// Keeping it separate from Notification avoids losing history during retries.
type DeliveryAttempt struct {
	ent.Schema
}

func (DeliveryAttempt) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "delivery_attempts"}}
}

func (DeliveryAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("notification_id", uuid.UUID{}),
		field.Int("attempt_number"),
		field.Enum("status").Values("succeeded", "retrying", "failed"),
		field.Int("http_status").Optional(),
		field.String("error_message").Optional(),
		field.Int64("duration_ms").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

// Indexes backs ListDeliveryAttempts: filter by notification_id, ordered by
// created_at. Without it the per-notification attempt history is a full scan.
func (DeliveryAttempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("notification_id", "created_at"),
	}
}
