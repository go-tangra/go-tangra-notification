package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/tx7do/go-crud/entgo/mixin"
)

// InternalMessage holds the schema definition for the InternalMessage entity.
type InternalMessage struct {
	ent.Schema
}

func (InternalMessage) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table:     "internal_messages",
			Charset:   "utf8mb4",
			Collation: "utf8mb4_bin",
		},
		entsql.WithComments(true),
		schema.Comment("Internal messages table"),
	}
}

// Fields of the InternalMessage.
func (InternalMessage) Fields() []ent.Field {
	return []ent.Field{
		field.String("title").
			Comment("Message title").
			Optional().
			Nillable(),

		field.String("content").
			Comment("Message content").
			Optional().
			Nillable(),

		field.Uint32("sender_id").
			Comment("Sender user ID").
			Nillable(),

		field.Uint32("category_id").
			Comment("Category ID").
			Optional().
			Nillable(),

		field.Enum("status").
			Comment("Message status").
			NamedValues(
				"Draft", "DRAFT",
				"Published", "PUBLISHED",
				"Scheduled", "SCHEDULED",
				"Revoked", "REVOKED",
				"Archived", "ARCHIVED",
				"Deleted", "DELETED",
			).
			Default("DRAFT").
			Optional().
			Nillable(),

		field.Enum("type").
			Comment("Message type").
			NamedValues(
				"Notification", "NOTIFICATION",
				"Private", "PRIVATE",
				"Group", "GROUP",
			).
			Default("NOTIFICATION").
			Optional().
			Nillable(),
	}
}

// Mixin of the InternalMessage.
func (InternalMessage) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.AutoIncrementId{},
		mixin.TimeAt{},
		mixin.OperatorID{},
		mixin.TenantID[uint32]{},
	}
}

func (InternalMessage) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "created_at").
			StorageKey("idx_internal_msg_tenant_created_at"),

		index.Fields("tenant_id", "status", "created_at").
			StorageKey("idx_internal_msg_tenant_status_created_at"),

		index.Fields("tenant_id", "sender_id", "created_at").
			StorageKey("idx_internal_msg_tenant_sender_created_at"),

		index.Fields("tenant_id", "category_id").
			StorageKey("idx_internal_msg_tenant_category"),

		index.Fields("tenant_id", "created_by", "created_at").
			StorageKey("idx_internal_msg_tenant_created_by_created_at"),
	}
}
