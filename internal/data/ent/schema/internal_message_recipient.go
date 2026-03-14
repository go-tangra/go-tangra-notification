package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/tx7do/go-crud/entgo/mixin"
)

// InternalMessageRecipient holds the schema definition for the InternalMessageRecipient entity.
type InternalMessageRecipient struct {
	ent.Schema
}

func (InternalMessageRecipient) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table:     "internal_message_recipients",
			Charset:   "utf8mb4",
			Collation: "utf8mb4_bin",
		},
		entsql.WithComments(true),
		schema.Comment("Internal message recipients table"),
	}
}

// Fields of the InternalMessageRecipient.
func (InternalMessageRecipient) Fields() []ent.Field {
	return []ent.Field{
		field.Uint32("message_id").
			Comment("Internal message content ID").
			Optional().
			Nillable(),

		field.Uint32("recipient_user_id").
			Comment("Recipient user ID").
			Optional().
			Nillable(),

		field.Enum("status").
			Comment("Message status").
			NamedValues(
				"Sent", "SENT",
				"Received", "RECEIVED",
				"Read", "READ",
				"Revoked", "REVOKED",
				"Deleted", "DELETED",
			).
			Optional().
			Nillable(),

		field.Time("received_at").
			Comment("Time when message arrived in user inbox").
			Optional().
			Nillable(),

		field.Time("read_at").
			Comment("Time when user read the message").
			Optional().
			Nillable(),
	}
}

// Mixin of the InternalMessageRecipient.
func (InternalMessageRecipient) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.AutoIncrementId{},
		mixin.TimeAt{},
		mixin.TenantID[uint32]{},
	}
}

func (InternalMessageRecipient) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "created_at").
			StorageKey("idx_internal_msg_recipient_tenant_created_at"),

		index.Fields("tenant_id", "message_id").
			StorageKey("idx_internal_msg_recipient_tenant_message"),

		index.Fields("tenant_id", "recipient_user_id", "created_at").
			StorageKey("idx_internal_msg_recipient_tenant_recipient_created_at"),

		index.Fields("tenant_id", "status", "created_at").
			StorageKey("idx_internal_msg_recipient_tenant_status_created_at"),

		index.Fields("recipient_user_id", "status", "created_at").
			StorageKey("idx_internal_msg_recipient_recipient_status_created_at"),

		index.Fields("message_id", "recipient_user_id").
			StorageKey("idx_internal_msg_recipient_message_recipient"),
	}
}
