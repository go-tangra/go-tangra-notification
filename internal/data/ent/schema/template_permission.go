package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/tx7do/go-crud/entgo/mixin"
)

// TemplatePermission holds the schema definition for the TemplatePermission entity.
// Implements Zanzibar-like permission tuples for fine-grained access control on templates.
type TemplatePermission struct {
	ent.Schema
}

// Annotations of the TemplatePermission.
func (TemplatePermission) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "notification_template_permissions"},
		entsql.WithComments(true),
	}
}

// Fields of the TemplatePermission.
func (TemplatePermission) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("resource_type").
			Values("RESOURCE_TYPE_UNSPECIFIED", "RESOURCE_TYPE_TEMPLATE", "RESOURCE_TYPE_CHANNEL").
			Comment("Type of resource (template or channel)"),

		field.String("resource_id").
			NotEmpty().
			MaxLen(36).
			Comment("ID of the resource (UUID)"),

		field.Enum("relation").
			Values("RELATION_UNSPECIFIED", "RELATION_OWNER", "RELATION_EDITOR", "RELATION_VIEWER", "RELATION_SHARER").
			Comment("Permission level (owner, editor, viewer, sharer)"),

		field.Enum("subject_type").
			Values("SUBJECT_TYPE_UNSPECIFIED", "SUBJECT_TYPE_USER", "SUBJECT_TYPE_ROLE", "SUBJECT_TYPE_CLIENT").
			Comment("Type of subject (user, role, or mTLS client)"),

		field.String("subject_id").
			NotEmpty().
			MaxLen(255).
			Comment("ID of the user, role, or mTLS client name"),

		field.Uint32("granted_by").
			Optional().
			Nillable().
			Comment("User ID who granted this permission"),

		field.Time("expires_at").
			Optional().
			Nillable().
			Comment("Optional expiration time for temporary access"),
	}
}

// Edges of the TemplatePermission.
func (TemplatePermission) Edges() []ent.Edge {
	return nil
}

// Mixin of the TemplatePermission.
func (TemplatePermission) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.Time{},
		mixin.TenantID[uint32]{},
	}
}

// Indexes of the TemplatePermission.
func (TemplatePermission) Indexes() []ent.Index {
	return []ent.Index{
		// Unique constraint for a permission tuple
		index.Fields("tenant_id", "resource_type", "resource_id", "relation", "subject_type", "subject_id").Unique(),
		// For looking up permissions on a resource
		index.Fields("tenant_id", "resource_type", "resource_id"),
		// For looking up permissions for a subject (tenant-scoped)
		index.Fields("tenant_id", "subject_type", "subject_id"),
		// For looking up by tenant
		index.Fields("tenant_id"),
		// For checking expiration
		index.Fields("expires_at"),
	}
}
