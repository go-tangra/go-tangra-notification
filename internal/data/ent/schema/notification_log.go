package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/tx7do/go-crud/entgo/mixin"
)

// NotificationLog holds the schema definition for the NotificationLog entity.
type NotificationLog struct {
	ent.Schema
}

func (NotificationLog) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "notification_logs"},
		entsql.WithComments(true),
	}
}

func (NotificationLog) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			NotEmpty().
			Unique().
			Comment("UUID primary key"),

		field.String("channel_id").
			NotEmpty().
			MaxLen(36).
			Comment("FK to notification_channels"),

		field.Enum("channel_type").
			Values("EMAIL", "SMS", "SLACK", "SSE").
			Comment("Channel type used"),

		field.String("template_id").
			NotEmpty().
			MaxLen(36).
			Comment("FK to notification_templates"),

		field.String("recipient").
			NotEmpty().
			MaxLen(512).
			Comment("Recipient address"),

		field.String("rendered_subject").
			MaxLen(1024).
			Default("").
			Comment("Rendered subject"),

		field.Text("rendered_body").
			Default("").
			Comment("Rendered body"),

		field.Enum("status").
			Values("PENDING", "SENT", "FAILED").
			Default("PENDING").
			Comment("Delivery status"),

		field.Text("error_message").
			Default("").
			Comment("Error message if delivery failed"),

		field.Time("sent_at").
			Optional().
			Nillable().
			Comment("When the notification was sent"),
	}
}

func (NotificationLog) Edges() []ent.Edge {
	return nil
}

func (NotificationLog) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.CreateBy{},
		mixin.Time{},
		mixin.TenantID[uint32]{},
	}
}

func (NotificationLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "channel_type"),
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "recipient"),
		index.Fields("tenant_id"),
		index.Fields("channel_id"),
		index.Fields("template_id"),
	}
}
