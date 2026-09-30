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

// PaymentExternalOrder stores only payment identifiers, monetary proof and recovery state; no customer PII.
type PaymentExternalOrder struct{ ent.Schema }

func (PaymentExternalOrder) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "payment_external_orders"}}
}
func (PaymentExternalOrder) Fields() []ent.Field {
	return []ent.Field{

		field.String("provider_key").MaxLen(30).NotEmpty(),
		field.String("website_id").MaxLen(128).NotEmpty(),
		field.String("external_order_id").MaxLen(128).NotEmpty(),
		field.String("order_number").MaxLen(64).Default(""),
		field.String("binding_method").MaxLen(30).Default("reference"),
		field.String("quote_hash").MaxLen(64).Default(""),
		field.Int64("local_order_id").Optional().Nillable(),
		field.String("checkout_reference").MaxLen(64).Default(""),
		field.String("currency").MaxLen(3).Default(""),
		field.Int64("total_minor").Default(0),
		field.Int64("refunded_minor").Default(0),
		field.Int64("credited_usd_units").Default(0),
		field.Int64("refund_target_usd_units").Default(0),
		field.Int64("recovered_usd_units").Default(0),
		field.Int64("debt_usd_units").Default(0),
		field.String("payment_state").MaxLen(30).Default("UNKNOWN"),
		field.String("status").MaxLen(30).Default("OBSERVED"),
		field.String("anomaly_code").MaxLen(64).Default(""),
		field.Time("paid_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("provider_modified_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_seen_at").Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),

		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}
func (PaymentExternalOrder) Indexes() []ent.Index {
	return []ent.Index{

		index.Fields("provider_key", "website_id", "external_order_id").Unique(),
		index.Fields("local_order_id").Unique(),
		index.Fields("provider_key", "website_id", "order_number"),
		index.Fields("provider_key", "website_id", "status"),
		index.Fields("anomaly_code"),
	}
}
