package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"time"
)

// PaymentExternalPayment stores only payment identifiers, monetary proof and recovery state; no customer PII.
type PaymentExternalPayment struct{ ent.Schema }

func (PaymentExternalPayment) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "payment_external_payments"}}
}
func (PaymentExternalPayment) Fields() []ent.Field {
	return []ent.Field{

		field.String("provider_key").MaxLen(30).NotEmpty(),
		field.String("website_id").MaxLen(128).NotEmpty(),
		field.String("payment_id").MaxLen(128).NotEmpty(),
		field.Int64("external_order_ledger_id"),
		field.String("currency").MaxLen(3).NotEmpty(),
		field.Int64("amount_minor"),
		field.Int64("refunded_minor").Default(0),
		field.Time("paid_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),

		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}
func (PaymentExternalPayment) Indexes() []ent.Index {
	return []ent.Index{

		index.Fields("provider_key", "website_id", "payment_id").Unique(),
		index.Fields("external_order_ledger_id"),
	}
}
