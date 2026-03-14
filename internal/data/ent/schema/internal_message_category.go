package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/tx7do/go-crud/entgo/mixin"
)

// InternalMessageCategory holds the schema definition for the InternalMessageCategory entity.
type InternalMessageCategory struct {
	ent.Schema
}

func (InternalMessageCategory) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table:     "internal_message_categories",
			Charset:   "utf8mb4",
			Collation: "utf8mb4_bin",
		},
		entsql.WithComments(true),
		schema.Comment("Internal message categories table"),
	}
}

// Fields of the InternalMessageCategory.
func (InternalMessageCategory) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			Comment("Name").
			NotEmpty().
			Optional().
			Nillable(),

		field.String("code").
			Comment("Code").
			NotEmpty().
			Optional().
			Nillable(),

		field.String("icon_url").
			Comment("Icon URL").
			Optional().
			Nillable(),
	}
}

// Mixin of the InternalMessageCategory.
func (InternalMessageCategory) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.AutoIncrementId{},
		mixin.TimeAt{},
		mixin.OperatorID{},
		mixin.IsEnabled{},
		mixin.SortOrder{},
		mixin.Remark{},
		mixin.TenantID[uint32]{},
	}
}

// Indexes of the InternalMessageCategory.
func (InternalMessageCategory) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "code").Unique().StorageKey("idx_internal_msg_cat_tenant_code"),
		index.Fields("tenant_id", "name").StorageKey("idx_internal_msg_cat_tenant_name"),
		index.Fields("tenant_id", "is_enabled").StorageKey("idx_internal_msg_cat_tenant_enabled"),
		index.Fields("tenant_id", "created_at").StorageKey("idx_internal_msg_cat_tenant_created_at"),
		index.Fields("tenant_id", "created_by").StorageKey("idx_internal_msg_cat_tenant_created_by"),
	}
}
