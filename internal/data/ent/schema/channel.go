package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/tx7do/go-crud/entgo/mixin"
)

// Channel holds the schema definition for the Channel entity.
type Channel struct {
	ent.Schema
}

func (Channel) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "notification_channels"},
		entsql.WithComments(true),
	}
}

func (Channel) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			NotEmpty().
			Unique().
			Comment("UUID primary key"),

		field.String("name").
			NotEmpty().
			MaxLen(255).
			Comment("Channel display name"),

		field.Enum("type").
			Values("EMAIL", "SMS", "SLACK", "SSE").
			Comment("Channel type"),

		field.Text("config").
			Default("{}").
			Comment("JSON-encoded channel configuration"),

		field.Bool("enabled").
			Default(true).
			Comment("Whether the channel is active"),

		field.Bool("is_default").
			Default(false).
			Comment("Whether this is the default channel for its type"),
	}
}

func (Channel) Edges() []ent.Edge {
	return nil
}

func (Channel) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.CreateBy{},
		mixin.UpdateBy{},
		mixin.Time{},
		mixin.TenantID[uint32]{},
	}
}

func (Channel) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "name").Unique(),
		index.Fields("tenant_id", "type"),
		index.Fields("tenant_id", "type", "is_default"),
		index.Fields("tenant_id"),
	}
}
