package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/tx7do/go-crud/entgo/mixin"
)

// Template holds the schema definition for the Template entity.
type Template struct {
	ent.Schema
}

func (Template) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "notification_templates"},
		entsql.WithComments(true),
	}
}

func (Template) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			NotEmpty().
			Unique().
			Comment("UUID primary key"),

		field.String("name").
			NotEmpty().
			MaxLen(255).
			Comment("Template name"),

		field.String("channel_id").
			NotEmpty().
			MaxLen(36).
			Comment("References notification_channels.id"),

		field.String("subject").
			NotEmpty().
			MaxLen(1024).
			Comment("Subject template (Go text/template)"),

		field.Text("body").
			NotEmpty().
			Comment("Body template (Go text/template or html/template for email)"),

		field.String("variables").
			Default("").
			MaxLen(2048).
			Comment("Comma-separated list of expected variable names"),

		field.Bool("is_default").
			Default(false).
			Comment("Whether this is the default template for its channel"),
	}
}

func (Template) Edges() []ent.Edge {
	return nil
}

func (Template) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.CreateBy{},
		mixin.UpdateBy{},
		mixin.Time{},
		mixin.TenantID[uint32]{},
	}
}

func (Template) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "name").Unique(),
		index.Fields("tenant_id", "channel_id"),
		index.Fields("tenant_id", "channel_id", "is_default"),
		index.Fields("tenant_id"),
	}
}
